package internode

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/cluster"
	"go.uber.org/zap"
)

// ConnectionState represents the state of a connection to a remote node
type ConnectionState int

const (
	StateNone ConnectionState = iota
	StateConnecting
	StateConnected
	StateRetrying
	StateDead
)

func (s ConnectionState) String() string {
	switch s {
	case StateNone:
		return "NONE"
	case StateConnecting:
		return "CONNECTING"
	case StateConnected:
		return "CONNECTED"
	case StateRetrying:
		return "RETRYING"
	case StateDead:
		return "DEAD"
	default:
		return "UNKNOWN"
	}
}

// ManagerTLSConfig holds configuration for optional transport encryption.
type ManagerTLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	CAFile   string `json:"ca_file"` // Certificate Authority for verifying peers
}

// ManagerConfig holds the configuration for the ConnectionManager.
type ManagerConfig struct {
	LocalNodeID       cluster.NodeID
	BindAddr          string
	BindPort          int
	HandshakeTimeout  time.Duration
	OutboundQueueSize int
	MaxMessageSize    uint32
	Logger            *zap.Logger

	// Optional TLS configuration for transport security
	TLS ManagerTLSConfig

	// Retry configuration - exponential backoff bounds
	InitialRetryDelay  time.Duration
	MaxRetryDelay      time.Duration
	RetryCheckInterval time.Duration
}

// DefaultManagerConfig returns a default configuration for the manager.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		HandshakeTimeout:   5 * time.Second,
		OutboundQueueSize:  256,
		MaxMessageSize:     512 * 1024 * 1024, // 512MB
		TLS:                ManagerTLSConfig{Enabled: false},
		InitialRetryDelay:  10 * time.Millisecond,
		MaxRetryDelay:      250 * time.Millisecond,
		RetryCheckInterval: 5 * time.Millisecond,
	}
}

// NodeConnectionConfig extracts NodeConnection-specific config from ManagerConfig
func (mc ManagerConfig) NodeConnectionConfig() NodeConnectionConfig {
	return NodeConnectionConfig{
		HandshakeTimeout:  mc.HandshakeTimeout,
		OutboundQueueSize: mc.OutboundQueueSize,
		MaxMessageSize:    mc.MaxMessageSize,
	}
}

// ConnectionManager defines the interface for managing inter-node TCP connections.
type ConnectionManager interface {
	Start(ctx context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) error
	Stop() error
	SendToNode(nodeID cluster.NodeID, data []byte) error
	EnsureConnection(nodeID cluster.NodeID, addr string, port int)
	DisconnectFromNode(nodeID cluster.NodeID)
	ConnectedNodes() []cluster.NodeID
	HandleNodeLeft(nodeID cluster.NodeID)
}

// manager is the concrete implementation of ConnectionManager.
type manager struct {
	config    ManagerConfig
	ctx       context.Context
	cancel    context.CancelFunc
	logger    *zap.Logger
	wg        sync.WaitGroup
	listener  net.Listener
	onMessage func(cluster.NodeID, []byte)
	tlsConfig *tls.Config

	nodeStates     *NodeStateManager
	retryScheduler *RetryScheduler
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager(config ManagerConfig) ConnectionManager {
	logger := config.Logger.Named("connection-manager")
	return &manager{
		config:         config,
		logger:         logger,
		nodeStates:     NewNodeStateManager(config, logger),
		retryScheduler: NewRetryScheduler(config, logger),
	}
}

// Start is non-blocking and initializes the manager's listener.
func (m *manager) Start(ctx context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.onMessage = onMessage

	// Load TLS config if enabled
	if m.config.TLS.Enabled {
		tlsConfig, err := loadTLSConfig(m.config.TLS)
		if err != nil {
			return fmt.Errorf("failed to load TLS configuration: %w", err)
		}
		m.tlsConfig = tlsConfig
		m.logger.Info("TLS encryption enabled for inter-node communication")
	}

	addr := fmt.Sprintf("%s:%d", m.config.BindAddr, m.config.BindPort)
	listener, err := m.listen(addr)
	if err != nil {
		return fmt.Errorf("failed to start listener on %s: %w", addr, err)
	}
	m.listener = listener
	m.logger.Info("TCP listener started", zap.String("address", addr))

	m.wg.Add(2)
	go func() { defer m.wg.Done(); m.acceptLoop() }()
	go func() { defer m.wg.Done(); m.retryLoop() }()

	return nil
}

// listen creates a listener, either plain TCP or TLS based on configuration.
func (m *manager) listen(addr string) (net.Listener, error) {
	if m.tlsConfig != nil {
		return tls.Listen("tcp", addr, m.tlsConfig)
	}
	return net.Listen("tcp", addr)
}

// Stop gracefully shuts down the manager.
func (m *manager) Stop() error {
	m.logger.Info("Stopping connection manager...")
	m.cancel() // Signal all goroutines to stop.
	if m.listener != nil {
		if err := m.listener.Close(); err != nil {
			m.logger.Error("Error closing listener", zap.Error(err))
		}
	}
	m.DisconnectFromNode("*") // Disconnect from all nodes.
	m.wg.Wait()
	m.logger.Info("Connection manager stopped.")
	return nil
}

// SendToNode queues data to be sent to a specific node. Never fails (Erlang semantics).
func (m *manager) SendToNode(nodeID cluster.NodeID, data []byte) error {
	m.nodeStates.QueueMessage(nodeID, data)
	go m.ensureConnection(nodeID)
	return nil
}

// EnsureConnection initiates a connection to a remote node if one doesn't exist.
func (m *manager) EnsureConnection(nodeID cluster.NodeID, addr string, port int) {
	m.nodeStates.UpdateNodeAddress(nodeID, addr, port)
	go m.ensureConnection(nodeID)
}

func (m *manager) DisconnectFromNode(nodeID cluster.NodeID) {
	if nodeID == "*" {
		for _, id := range m.nodeStates.GetConnectedNodes() {
			m.nodeStates.CloseNodeConnection(id)
		}
		return
	}
	m.nodeStates.CloseNodeConnection(nodeID)
}

func (m *manager) ConnectedNodes() []cluster.NodeID { return m.nodeStates.GetConnectedNodes() }

func (m *manager) HandleNodeLeft(nodeID cluster.NodeID) {
	m.logger.Info("Node left cluster", zap.String("node", string(nodeID)))
	m.nodeStates.MarkNodeDead(nodeID)
	m.retryScheduler.ClearRetryState(nodeID)
}

func (m *manager) ensureConnection(nodeID cluster.NodeID) {
	_, currentState := m.nodeStates.GetNodeConnection(nodeID)
	if currentState == StateConnected || currentState == StateConnecting || currentState == StateDead {
		return
	}
	if !m.nodeStates.TryLockConnection(nodeID) {
		return
	}
	defer m.nodeStates.UnlockConnection(nodeID)
	addr, port, hasAddr := m.nodeStates.GetNodeAddress(nodeID)
	if !hasAddr {
		return
	}
	m.nodeStates.SetNodeState(nodeID, StateConnecting)
	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.attemptConnection(nodeID, addr, port) }()
}

// attemptConnection tries to establish a connection to a node.
func (m *manager) attemptConnection(nodeID cluster.NodeID, addr string, port int) {
	fullAddr := fmt.Sprintf("%s:%d", addr, port)
	retryCount, _, _ := m.retryScheduler.GetRetryInfo(nodeID)

	var conn net.Conn
	var err error

	if m.tlsConfig != nil {
		dialer := &net.Dialer{Timeout: m.config.HandshakeTimeout}
		conn, err = tls.DialWithDialer(dialer, "tcp", fullAddr, m.tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", fullAddr, m.config.HandshakeTimeout)
	}

	if err != nil {
		if retryCount < 3 { // Only log first few attempts to reduce noise
			m.logger.Debug("Failed to connect", zap.String("node", string(nodeID)), zap.Error(err))
		}
		m.handleConnectionFailure(nodeID, err)
		return
	}

	nodeConn := newNodeConnection(conn, nodeID, m.config.NodeConnectionConfig(), m.logger)
	if err := nodeConn.performHandshake(m.ctx, m.config.LocalNodeID, true); err != nil {
		if retryCount < 3 { // Only log first few attempts to reduce noise
			m.logger.Debug("Outbound handshake failed", zap.String("node", string(nodeID)), zap.Error(err))
		}
		nodeConn.Close()
		m.handleConnectionFailure(nodeID, err)
		return
	}
	m.handleConnectionSuccess(nodeID, nodeConn)
}

func (m *manager) handleConnectionSuccess(nodeID cluster.NodeID, conn *NodeConnection) {
	m.logger.Info("Connection established", zap.String("node", string(nodeID)))
	m.nodeStates.SetNodeConnection(nodeID, conn, StateConnected)
	m.retryScheduler.ResetRetry(nodeID)

	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.drainQueueToConnection(nodeID, conn) }()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		conn.Run(m.ctx, m.onMessage)
		m.handleConnectionFailure(nodeID, fmt.Errorf("connection terminated"))
	}()
}

func (m *manager) handleConnectionFailure(nodeID cluster.NodeID, err error) {
	conn, currentState := m.nodeStates.GetNodeConnection(nodeID)
	if conn != nil {
		undelivered := conn.ExtractPendingMessages()
		conn.Close()
		if len(undelivered) > 0 {
			m.nodeStates.RequeueMessages(nodeID, undelivered)
		}
	}
	if currentState == StateDead {
		return
	}
	m.nodeStates.SetNodeConnection(nodeID, nil, StateRetrying)
	m.retryScheduler.ScheduleRetry(nodeID)
}

// drainQueueToConnection efficiently drains messages using notification channel.
// NO MORE 1ms SLEEP LOOP!
func (m *manager) drainQueueToConnection(nodeID cluster.NodeID, conn *NodeConnection) {
	notifyChan := m.nodeStates.GetMessageNotifier(nodeID)
	if notifyChan == nil {
		return
	}

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-notifyChan:
			// Batch drain messages to reduce mutex contention
			messages := m.nodeStates.DrainMessages(nodeID, 32) // Batch size 32
			if len(messages) == 0 {
				continue
			}

			currentConn, state := m.nodeStates.GetNodeConnection(nodeID)
			if state != StateConnected || currentConn != conn {
				// Requeue the messages we just drained
				m.nodeStates.RequeueMessages(nodeID, messages)
				return
			}

			// Send all messages in the batch
			for _, data := range messages {
				if err := conn.Send(data); err != nil {
					return
				}
			}
		}
	}
}

func (m *manager) retryLoop() {
	ticker := time.NewTicker(m.retryScheduler.GetCheckInterval())
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkRetries()
		}
	}
}

func (m *manager) checkRetries() {
	for _, nodeID := range m.retryScheduler.GetNodesReadyForRetry() {
		m.retryScheduler.MarkRetryInProgress(nodeID)
		go m.ensureConnection(nodeID)
	}
}

func (m *manager) acceptLoop() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			if m.ctx.Err() != nil {
				return
			}
			m.logger.Error("Failed to accept connection", zap.Error(err))
			continue
		}
		m.wg.Add(1)
		go func() { defer m.wg.Done(); m.handleInboundConnection(conn) }()
	}
}

func (m *manager) handleInboundConnection(conn net.Conn) {
	nodeConn := newNodeConnection(conn, "unknown", m.config.NodeConnectionConfig(), m.logger)
	if err := nodeConn.performHandshake(m.ctx, m.config.LocalNodeID, false); err != nil {
		m.logger.Warn("Inbound handshake failed", zap.Error(err), zap.String("remote_addr", conn.RemoteAddr().String()))
		nodeConn.Close()
		return
	}
	remoteNodeID := nodeConn.RemoteNodeID()
	if m.shouldDropInbound(remoteNodeID) {
		nodeConn.Close()
		return
	}
	existingConn, _ := m.nodeStates.GetNodeConnection(remoteNodeID)
	if existingConn != nil {
		nodeConn.Close()
		return
	}
	m.handleConnectionSuccess(remoteNodeID, nodeConn)
}

func (m *manager) shouldDropInbound(remoteNodeID cluster.NodeID) bool {
	return strings.Compare(string(m.config.LocalNodeID), string(remoteNodeID)) > 0
}

// loadTLSConfig creates a client and server TLS config for mutual TLS (mTLS).
func loadTLSConfig(cfg ManagerTLSConfig) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("could not load key pair: %w", err)
	}

	caCertPool := x509.NewCertPool()
	caCert, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("could not read ca certificate: %w", err)
	}
	if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
		return nil, fmt.Errorf("failed to append ca certs")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},        // For server side and client auth
		RootCAs:      caCertPool,                     // For client to verify server
		ClientCAs:    caCertPool,                     // For server to verify client
		ClientAuth:   tls.RequireAndVerifyClientCert, // Enforce mTLS
		MinVersion:   tls.VersionTLS12,
	}, nil
}
