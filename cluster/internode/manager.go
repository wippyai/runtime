package internode

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/cluster"
	"go.uber.org/zap"
)

const (
	DefaultPortRangeStart = 7950
	DefaultPortRangeEnd   = 7959
)

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

type ManagerTLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	CAFile   string `json:"ca_file"`
}

type ManagerConfig struct {
	LocalNodeID       cluster.NodeID
	BindAddr          string
	BindPort          int
	AutoPort          bool
	HandshakeTimeout  time.Duration
	OutboundQueueSize int
	MaxMessageSize    uint32
	Logger            *zap.Logger
	TLS               ManagerTLSConfig
	InitialRetryDelay time.Duration
	MaxRetryDelay     time.Duration
	DrainBatchSize    int
	CommandQueueSize  int
	MaxRetryAttempts  int
}

func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		HandshakeTimeout:  5 * time.Second,
		OutboundQueueSize: 256,
		MaxMessageSize:    512 * 1024 * 1024,
		TLS:               ManagerTLSConfig{Enabled: false},
		InitialRetryDelay: 10 * time.Millisecond,
		MaxRetryDelay:     5 * time.Second,
		AutoPort:          true,
		BindPort:          DefaultPortRangeStart,
		DrainBatchSize:    32,
		CommandQueueSize:  256,
		MaxRetryAttempts:  10,
	}
}

func (mc ManagerConfig) NodeConnectionConfig() NodeConnectionConfig {
	return NodeConnectionConfig{
		HandshakeTimeout: mc.HandshakeTimeout,
		MaxMessageSize:   mc.MaxMessageSize,
	}
}

type nodeCommand struct {
	Type commandType
	Data interface{}
}

type commandType int

const (
	cmdConnect commandType = iota
	cmdConnected
	cmdDisconnected
	cmdKill
)

type connectData struct {
	Addr string
	Port int
}

type connectedData struct {
	Connection *NodeConnection
}

type disconnectedData struct {
	Error       error
	ShouldRetry bool
}

type nodeControlLoop struct {
	nodeID     cluster.NodeID
	manager    *manager
	commands   chan nodeCommand
	state      ConnectionState
	connection *NodeConnection
	addr       string
	port       int
	retryCount int
	retryDelay time.Duration
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *zap.Logger
	isOutbound bool
}

type ConnectionManager interface {
	Start(ctx context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) error
	Stop() error
	SendToNode(nodeID cluster.NodeID, data []byte) error
	EnsureConnection(nodeID cluster.NodeID, addr string, port int)
	DisconnectFromNode(nodeID cluster.NodeID)
	ConnectedNodes() []cluster.NodeID
	GetListenPort() int

	// Lifecycle methods driven by cluster events
	AddManagedNode(nodeID cluster.NodeID)
	RemoveManagedNode(nodeID cluster.NodeID)
}

type manager struct {
	config         ManagerConfig
	ctx            context.Context
	cancel         context.CancelFunc
	logger         *zap.Logger
	wg             sync.WaitGroup
	listener       net.Listener
	onMessage      func(cluster.NodeID, []byte)
	tlsConfig      *tls.Config
	nodeStates     *NodeStateManager
	controlLoops   map[cluster.NodeID]*nodeControlLoop
	controlLoopsMu sync.Mutex
	actualPort     int
}

func NewConnectionManager(config ManagerConfig) ConnectionManager {
	logger := config.Logger.Named("conn")
	return &manager{
		config:       config,
		logger:       logger,
		nodeStates:   NewNodeStateManager(config, logger),
		controlLoops: make(map[cluster.NodeID]*nodeControlLoop),
	}
}

func (m *manager) Start(ctx context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.onMessage = onMessage

	if m.config.TLS.Enabled {
		tlsConfig, err := loadTLSConfig(m.config.TLS)
		if err != nil {
			return fmt.Errorf("failed to load TLS configuration: %w", err)
		}
		m.tlsConfig = tlsConfig
	}

	listener, actualPort, err := m.startListener()
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	m.listener = listener
	m.actualPort = actualPort
	m.logger.Info("TCP listener started", zap.String("address", m.config.BindAddr), zap.Int("port", actualPort))

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.acceptLoop()
	}()

	return nil
}

func (m *manager) Stop() error {
	m.logger.Info("Stopping connection manager...")
	m.cancel()
	if m.listener != nil {
		_ = m.listener.Close()
	}

	m.controlLoopsMu.Lock()
	for _, loop := range m.controlLoops {
		loop.cancel()
	}
	m.controlLoops = make(map[cluster.NodeID]*nodeControlLoop)
	m.controlLoopsMu.Unlock()

	m.wg.Wait()
	m.logger.Info("Connection manager stopped")
	return nil
}

func (m *manager) SendToNode(nodeID cluster.NodeID, data []byte) error {
	err := m.nodeStates.QueueMessage(nodeID, data)
	if err != nil {
		if errors.Is(err, ErrNodeNotManaged) {
			m.logger.Warn("Dropping message for non-existent or unmanaged node", zap.String("target_node", string(nodeID)))
			// Return nil to not propagate "node not found" errors for fire-and-forget sends.
			// The caller can check the cluster membership itself if a response is required.
			return nil
		}
		return err
	}
	return nil
}

func (m *manager) EnsureConnection(nodeID cluster.NodeID, addr string, port int) {
	if m.nodeStates.GetNodeState(nodeID) == nil {
		m.logger.Error("EnsureConnection called for an unmanaged node. This is a logic error.", zap.String("node", string(nodeID)))
		return
	}

	_, currentState := m.nodeStates.GetNodeConnection(nodeID)
	if currentState == StateConnected {
		return
	}

	if !m.shouldInitiateConnection(nodeID) {
		return
	}

	m.nodeStates.UpdateNodeAddress(nodeID, addr, port)
	m.sendCommand(nodeID, nodeCommand{
		Type: cmdConnect,
		Data: connectData{Addr: addr, Port: port},
	})
}

func (m *manager) DisconnectFromNode(nodeID cluster.NodeID) {
	m.sendCommand(nodeID, nodeCommand{Type: cmdKill})
}

func (m *manager) ConnectedNodes() []cluster.NodeID {
	return m.nodeStates.GetConnectedNodes()
}

func (m *manager) AddManagedNode(nodeID cluster.NodeID) {
	m.logger.Info("Adding new managed node", zap.String("node", string(nodeID)))
	m.nodeStates.CreateNodeState(nodeID)
}

func (m *manager) RemoveManagedNode(nodeID cluster.NodeID) {
	m.logger.Info("Removing managed node", zap.String("node", string(nodeID)))
	m.sendCommand(nodeID, nodeCommand{Type: cmdKill})
	m.nodeStates.RemoveNodeState(nodeID)
}

func (m *manager) GetListenPort() int {
	return m.actualPort
}

func (m *manager) sendCommand(nodeID cluster.NodeID, cmd nodeCommand) {
	m.controlLoopsMu.Lock()
	loop, exists := m.controlLoops[nodeID]
	if !exists {
		// Before creating a loop, verify the underlying state exists.
		if m.nodeStates.GetNodeState(nodeID) == nil {
			m.controlLoopsMu.Unlock()
			m.logger.Error("Attempted to create control loop for unmanaged node", zap.String("node", string(nodeID)))
			return
		}

		ctx, cancel := context.WithCancel(m.ctx)
		loop = &nodeControlLoop{
			nodeID:     nodeID,
			manager:    m,
			commands:   make(chan nodeCommand, m.config.CommandQueueSize),
			state:      StateNone,
			ctx:        ctx,
			cancel:     cancel,
			logger:     m.logger.With(zap.String("node", string(nodeID))),
			retryDelay: m.config.InitialRetryDelay,
		}
		m.controlLoops[nodeID] = loop

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer m.cleanupControlLoop(nodeID)
			loop.run()
		}()
	}
	m.controlLoopsMu.Unlock()

	select {
	case loop.commands <- cmd:
	case <-loop.ctx.Done():
	}
}

func (m *manager) cleanupControlLoop(nodeID cluster.NodeID) {
	m.controlLoopsMu.Lock()
	delete(m.controlLoops, nodeID)
	m.controlLoopsMu.Unlock()
	m.logger.Debug("Control loop cleaned up", zap.String("node", string(nodeID)))
}

func (loop *nodeControlLoop) run() {
	loop.logger.Debug("Control loop started")
	defer loop.logger.Debug("Control loop stopped")

	// The NodeState is guaranteed to exist by the new design, so GetMessageNotifier will return a valid channel.
	messageNotifier := loop.manager.nodeStates.GetMessageNotifier(loop.nodeID)
	if messageNotifier == nil {
		// This path indicates a severe logic error in the new design.
		loop.logger.Error("Failed to get message notifier for a managed node; loop will be ineffective.",
			zap.String("node", string(loop.nodeID)))
		messageNotifier = make(<-chan struct{}) // Prevent nil-channel-select panic.
	}

	for {
		select {
		case <-loop.ctx.Done():
			loop.cleanup()
			return

		case cmd := <-loop.commands:
			if loop.handleCommand(cmd) {
				loop.cleanup()
				return
			}

		case <-messageNotifier:
			if loop.state == StateConnected && loop.connection != nil {
				loop.drainMessages()
			}
		}
	}
}

func (loop *nodeControlLoop) handleCommand(cmd nodeCommand) bool {
	switch cmd.Type {
	case cmdConnect:
		data, _ := cmd.Data.(connectData)
		loop.handleConnect(data)
	case cmdConnected:
		data, _ := cmd.Data.(connectedData)
		loop.handleConnected(data)
	case cmdDisconnected:
		data, _ := cmd.Data.(disconnectedData)
		loop.handleDisconnected(data)
	case cmdKill:
		loop.handleKill()
		return true
	}
	return false
}

func (loop *nodeControlLoop) handleConnect(data connectData) {
	loop.addr = data.Addr
	loop.port = data.Port
	if loop.state == StateConnecting || loop.state == StateConnected || loop.state == StateDead {
		return
	}
	loop.state = StateConnecting
	loop.isOutbound = true
	loop.manager.nodeStates.SetNodeState(loop.nodeID, loop.state)

	loop.manager.wg.Add(1)
	go func() {
		defer loop.manager.wg.Done()
		loop.attemptConnection()
	}()
}

func (loop *nodeControlLoop) handleConnected(data connectedData) {
	if loop.state == StateConnected {
		if data.Connection != nil {
			data.Connection.Close()
		}
		return
	}

	if loop.state != StateConnecting && loop.state != StateNone && loop.state != StateRetrying {
		if data.Connection != nil {
			data.Connection.Close()
		}
		return
	}

	loop.connection = data.Connection
	loop.state = StateConnected
	loop.retryCount = 0
	loop.retryDelay = loop.manager.config.InitialRetryDelay
	loop.manager.nodeStates.SetNodeConnection(loop.nodeID, loop.connection, loop.state)
	loop.logger.Info("Connection established successfully", zap.Bool("is_outbound", loop.isOutbound))

	loop.manager.wg.Add(1)
	go func() {
		defer loop.manager.wg.Done()
		loop.monitorConnection()
	}()

	loop.drainMessages()
}

func (loop *nodeControlLoop) handleDisconnected(data disconnectedData) {
	if loop.state == StateDead {
		return
	}
	if loop.connection != nil {
		pending := loop.connection.ExtractPendingMessages()
		loop.connection.Close()
		loop.connection = nil
		if len(pending) > 0 {
			loop.manager.nodeStates.RequeueMessages(loop.nodeID, pending)
		}
	}
	loop.manager.nodeStates.SetNodeConnection(loop.nodeID, nil, StateNone)

	if data.ShouldRetry && loop.isOutbound && loop.retryCount < loop.manager.config.MaxRetryAttempts {
		loop.state = StateRetrying
		loop.retryCount++
		if loop.retryDelay < loop.manager.config.MaxRetryDelay {
			loop.retryDelay *= 2
		}
		time.AfterFunc(loop.retryDelay, func() {
			addr, port, hasAddr := loop.manager.nodeStates.GetNodeAddress(loop.nodeID)
			if !hasAddr {
				addr, port = loop.addr, loop.port
			}
			loop.sendCommandToSelf(nodeCommand{Type: cmdConnect, Data: connectData{Addr: addr, Port: port}})
		})
	} else {
		loop.state = StateNone
	}
}

func (loop *nodeControlLoop) handleKill() {
	loop.state = StateDead
}

func (loop *nodeControlLoop) cleanup() {
	if loop.connection != nil {
		pending := loop.connection.ExtractPendingMessages()
		loop.connection.Close()
		loop.connection = nil
		if len(pending) > 0 {
			loop.manager.nodeStates.RequeueMessages(loop.nodeID, pending)
		}
	}
	loop.manager.nodeStates.SetNodeConnection(loop.nodeID, nil, StateNone)
}

func (loop *nodeControlLoop) sendCommandToSelf(cmd nodeCommand) {
	select {
	case loop.commands <- cmd:
	case <-loop.ctx.Done():
	}
}

func (loop *nodeControlLoop) attemptConnection() {
	if loop.ctx.Err() != nil {
		return
	}
	addr := fmt.Sprintf("%s:%d", loop.addr, loop.port)
	var conn net.Conn
	var err error
	if loop.manager.tlsConfig != nil {
		dialer := &net.Dialer{Timeout: loop.manager.config.HandshakeTimeout}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, loop.manager.tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", addr, loop.manager.config.HandshakeTimeout)
	}
	if err != nil {
		loop.sendDisconnected(err, true)
		return
	}
	nodeConn, err := PerformClientHandshake(conn, loop.manager.config.NodeConnectionConfig(), loop.logger, loop.manager.config.LocalNodeID, loop.nodeID)
	if err != nil {
		loop.sendDisconnected(err, true)
		return
	}
	loop.sendCommandToSelf(nodeCommand{Type: cmdConnected, Data: connectedData{Connection: nodeConn}})
}

func (loop *nodeControlLoop) monitorConnection() {
	if loop.connection == nil {
		return
	}
	err := loop.connection.Run(func(msg []byte) {
		loop.manager.onMessage(loop.nodeID, msg)
	})
	shouldRetry := false
	if err != nil {
		var connErr *ConnectionError
		if errors.As(err, &connErr) {
			shouldRetry = connErr.ShouldRetry()
		}
	}
	loop.sendDisconnected(err, shouldRetry)
}

func (loop *nodeControlLoop) drainMessages() {
	if loop.connection == nil {
		return
	}
	messages := loop.manager.nodeStates.DrainMessages(loop.nodeID, loop.manager.config.DrainBatchSize)
	if len(messages) == 0 {
		return
	}
	for i, data := range messages {
		if err := loop.connection.Send(data); err != nil {
			loop.logger.Error("Failed to send message, will be requeued", zap.Error(err))
			loop.manager.nodeStates.RequeueMessages(loop.nodeID, messages[i:])
			break
		}
	}
}

func (loop *nodeControlLoop) sendDisconnected(err error, shouldRetry bool) {
	loop.sendCommandToSelf(nodeCommand{Type: cmdDisconnected, Data: disconnectedData{Error: err, ShouldRetry: shouldRetry}})
}

func (m *manager) startListener() (net.Listener, int, error) {
	if m.config.AutoPort {
		return m.tryPortRange()
	}
	addr := fmt.Sprintf("%s:%d", m.config.BindAddr, m.config.BindPort)
	listener, err := m.listen(addr)
	return listener, m.config.BindPort, err
}

func (m *manager) tryPortRange() (net.Listener, int, error) {
	startPort := m.config.BindPort
	if startPort == 0 {
		startPort = DefaultPortRangeStart
	}
	for port := startPort; port <= DefaultPortRangeEnd; port++ {
		addr := fmt.Sprintf("%s:%d", m.config.BindAddr, port)
		if listener, err := m.listen(addr); err == nil {
			return listener, port, nil
		}
	}
	addr := fmt.Sprintf("%s:0", m.config.BindAddr)
	listener, err := m.listen(addr)
	if err != nil {
		return nil, 0, err
	}
	return listener, listener.Addr().(*net.TCPAddr).Port, nil
}

func (m *manager) listen(addr string) (net.Listener, error) {
	if m.tlsConfig != nil {
		return tls.Listen("tcp", addr, m.tlsConfig)
	}
	return net.Listen("tcp", addr)
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
		go func() {
			defer m.wg.Done()
			m.handleInboundConnection(conn)
		}()
	}
}

func (m *manager) handleInboundConnection(conn net.Conn) {
	nodeConn, err := PerformServerHandshake(conn, m.config.NodeConnectionConfig(), m.logger, m.config.LocalNodeID)
	if err != nil {
		m.logger.Warn("Inbound handshake failed", zap.Error(err), zap.String("remote_addr", conn.RemoteAddr().String()))
		return
	}
	remoteNodeID := nodeConn.RemoteNodeID()

	if m.nodeStates.GetNodeState(remoteNodeID) == nil {
		m.logger.Warn("Received connection from an unmanaged/unknown node", zap.String("node", string(remoteNodeID)))
		nodeConn.Close()
		return
	}

	_, currentState := m.nodeStates.GetNodeConnection(remoteNodeID)
	if currentState == StateConnected {
		m.logger.Debug("Already connected, dropping new inbound connection", zap.String("node", string(remoteNodeID)))
		nodeConn.Close()
		return
	}

	if m.shouldDropInbound(remoteNodeID) {
		m.logger.Debug("Dropping inbound connection due to tie-breaking", zap.String("node", string(remoteNodeID)))
		nodeConn.Close()
		return
	}

	m.sendCommand(remoteNodeID, nodeCommand{
		Type: cmdConnected,
		Data: connectedData{Connection: nodeConn},
	})
}

func (m *manager) shouldInitiateConnection(remoteNodeID cluster.NodeID) bool {
	return strings.Compare(string(m.config.LocalNodeID), string(remoteNodeID)) < 0
}

func (m *manager) shouldDropInbound(remoteNodeID cluster.NodeID) bool {
	return m.shouldInitiateConnection(remoteNodeID)
}

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
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}, nil
}
