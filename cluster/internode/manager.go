package internode

import (
	"context"
	"fmt"
	"net"
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

// ManagerConfig holds the configuration for the ConnectionManager.
type ManagerConfig struct {
	LocalNodeID       cluster.NodeID
	BindAddr          string
	BindPort          int
	HandshakeTimeout  time.Duration
	OutboundQueueSize int
	MaxMessageSize    uint32
	Logger            *zap.Logger

	// Retry configuration - exponential backoff bounds
	InitialRetryDelay  time.Duration // Initial delay between retry attempts (default: 10ms)
	MaxRetryDelay      time.Duration // Maximum delay between retry attempts (default: 250ms)
	RetryCheckInterval time.Duration // How often to check for nodes needing retry (default: 5ms)
}

// DefaultManagerConfig returns a default configuration for the manager.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		HandshakeTimeout:   5 * time.Second,
		OutboundQueueSize:  256,
		MaxMessageSize:     512 * 1024 * 1024, // 512MB
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
	// Start begins the manager's operations, including listening for inbound connections.
	// This method is non-blocking.
	Start(ctx context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) error
	// Stop gracefully shuts down the manager and all active connections.
	Stop() error
	// SendToNode sends data to a specific node, never fails (Erlang semantics).
	SendToNode(nodeID cluster.NodeID, data []byte) error
	// EnsureConnection initiates a connection to a node if one does not already exist.
	// This method is non-blocking and idempotent.
	EnsureConnection(nodeID cluster.NodeID, addr string, port int)
	// DisconnectFromNode explicitly closes the connection to a specific node.
	DisconnectFromNode(nodeID cluster.NodeID)
	// ConnectedNodes returns a slice of IDs for all currently connected nodes.
	ConnectedNodes() []cluster.NodeID
	// HandleNodeLeft should be called when a node leaves the cluster to clear its queue.
	HandleNodeLeft(nodeID cluster.NodeID)
}

// manager is the concrete implementation of ConnectionManager.
// It coordinates between NodeStateManager and RetryScheduler to provide reliable message delivery.
type manager struct {
	config    ManagerConfig
	ctx       context.Context
	cancel    context.CancelFunc
	logger    *zap.Logger
	wg        sync.WaitGroup
	listener  net.Listener
	onMessage func(cluster.NodeID, []byte)

	// Extracted components
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

	addr := fmt.Sprintf("%s:%d", m.config.BindAddr, m.config.BindPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start TCP listener on %s: %w", addr, err)
	}
	m.listener = listener
	m.logger.Info("TCP listener started", zap.String("address", addr))

	m.wg.Add(2)
	go func() {
		defer m.wg.Done()
		m.acceptLoop()
	}()
	go func() {
		defer m.wg.Done()
		m.retryLoop()
	}()

	return nil
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
	// Always queue the message (Erlang semantics - never fails)
	m.nodeStates.QueueMessage(nodeID, data)

	// Ensure we have a connection (non-blocking)
	go m.ensureConnection(nodeID)

	return nil // Never fails!
}

// EnsureConnection initiates a connection to a remote node if one doesn't exist.
func (m *manager) EnsureConnection(nodeID cluster.NodeID, addr string, port int) {
	// Update node address info
	m.nodeStates.UpdateNodeAddress(nodeID, addr, port)

	// Trigger connection attempt
	go m.ensureConnection(nodeID)
}

// DisconnectFromNode closes a connection. Accepts "*" to disconnect all.
func (m *manager) DisconnectFromNode(nodeID cluster.NodeID) {
	if nodeID == "*" {
		connected := m.nodeStates.GetConnectedNodes()
		for _, id := range connected {
			m.nodeStates.CloseNodeConnection(id)
		}
		return
	}

	m.nodeStates.CloseNodeConnection(nodeID)
}

// ConnectedNodes returns the list of currently connected node IDs.
func (m *manager) ConnectedNodes() []cluster.NodeID {
	return m.nodeStates.GetConnectedNodes()
}

// HandleNodeLeft clears the message queue for a node that has left the cluster.
func (m *manager) HandleNodeLeft(nodeID cluster.NodeID) {
	m.logger.Info("Node left cluster", zap.String("node", string(nodeID)))

	// Mark node as dead and clear queue
	m.nodeStates.MarkNodeDead(nodeID)

	// Clear retry state
	m.retryScheduler.ClearRetryState(nodeID)
}

// ensureConnection ensures there's an active connection to the specified node.
func (m *manager) ensureConnection(nodeID cluster.NodeID) {
	// Check current state
	_, currentState := m.nodeStates.GetNodeConnection(nodeID)

	if currentState == StateConnected || currentState == StateConnecting {
		return
	}

	if currentState == StateDead {
		return // Don't connect to dead nodes
	}

	// Prevent multiple concurrent connection attempts
	if !m.nodeStates.TryLockConnection(nodeID) {
		return
	}
	defer m.nodeStates.UnlockConnection(nodeID)

	// Check if we have addressing info
	addr, port, hasAddr := m.nodeStates.GetNodeAddress(nodeID)
	if !hasAddr {
		return
	}

	// Start connection attempt
	m.nodeStates.SetNodeState(nodeID, StateConnecting)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.attemptConnection(nodeID, addr, port)
	}()
}

// attemptConnection tries to establish a connection to a node.
func (m *manager) attemptConnection(nodeID cluster.NodeID, addr string, port int) {
	fullAddr := fmt.Sprintf("%s:%d", addr, port)

	retryCount, _, _ := m.retryScheduler.GetRetryInfo(nodeID)
	m.logger.Debug("Attempting connection",
		zap.String("node", string(nodeID)),
		zap.String("addr", fullAddr),
		zap.Int("attempt", retryCount+1))

	conn, err := net.DialTimeout("tcp", fullAddr, m.config.HandshakeTimeout)
	if err != nil {
		m.logger.Debug("Failed to connect",
			zap.String("node", string(nodeID)),
			zap.Error(err))
		m.handleConnectionFailure(nodeID, err)
		return
	}

	nodeConn := newNodeConnection(conn, nodeID, m.config.NodeConnectionConfig(), m.logger)
	if err := nodeConn.performHandshake(m.ctx, m.config.LocalNodeID, true); err != nil {
		m.logger.Debug("Outbound handshake failed",
			zap.String("node", string(nodeID)),
			zap.Error(err))
		nodeConn.Close()
		m.handleConnectionFailure(nodeID, err)
		return
	}

	// Connection successful
	m.handleConnectionSuccess(nodeID, nodeConn)
}

// handleConnectionSuccess processes a successful connection establishment.
func (m *manager) handleConnectionSuccess(nodeID cluster.NodeID, conn *NodeConnection) {
	m.logger.Info("Connection established", zap.String("node", string(nodeID)))

	// Update state
	m.nodeStates.SetNodeConnection(nodeID, conn, StateConnected)

	// Reset retry state
	m.retryScheduler.ResetRetry(nodeID)

	// Start draining the message queue to the connection
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.drainQueueToConnection(nodeID, conn)
	}()

	// Start the connection's run loops
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		conn.Run(m.ctx, m.onMessage)
		// When Run() returns, the connection has failed
		m.handleConnectionFailure(nodeID, fmt.Errorf("connection terminated"))
	}()
}

// handleConnectionFailure processes a connection failure and schedules retry.
func (m *manager) handleConnectionFailure(nodeID cluster.NodeID, err error) {
	m.logger.Debug("Connection failed",
		zap.String("node", string(nodeID)),
		zap.Error(err))

	// Get current connection to extract messages
	conn, currentState := m.nodeStates.GetNodeConnection(nodeID)

	// Extract any pending messages from the failed connection
	if conn != nil {
		undelivered := conn.ExtractPendingMessages()
		conn.Close()

		// Requeue undelivered messages at the front to preserve order
		if len(undelivered) > 0 {
			m.nodeStates.RequeueMessages(nodeID, undelivered)
		}
	}

	// Don't retry if node is dead
	if currentState == StateDead {
		return
	}

	// Update state and schedule retry
	m.nodeStates.SetNodeConnection(nodeID, nil, StateRetrying)
	m.retryScheduler.ScheduleRetry(nodeID)
}

// drainQueueToConnection continuously drains the message queue to an active connection.
func (m *manager) drainQueueToConnection(nodeID cluster.NodeID, conn *NodeConnection) {
	m.logger.Debug("Started queue draining", zap.String("node", string(nodeID)))

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
			// Check if connection is still active
			currentConn, state := m.nodeStates.GetNodeConnection(nodeID)
			if state != StateConnected || currentConn != conn {
				return
			}

			// Get next message from queue
			data := m.nodeStates.GetNextMessage(nodeID)
			if data == nil {
				// No messages, wait a bit before checking again
				time.Sleep(time.Millisecond)
				continue
			}

			// Send message to connection
			if err := conn.Send(data); err != nil {
				// Connection failed, message will be recovered by handleConnectionFailure
				m.logger.Debug("Failed to send queued message",
					zap.String("node", string(nodeID)),
					zap.Error(err))
				return
			}
		}
	}
}

// retryLoop periodically checks for nodes that need retry attempts.
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

// checkRetries checks all nodes for retry attempts that are due.
func (m *manager) checkRetries() {
	readyNodes := m.retryScheduler.GetNodesReadyForRetry()

	// Trigger retry attempts
	for _, nodeID := range readyNodes {
		// Mark retry in progress to prevent duplicate attempts
		m.retryScheduler.MarkRetryInProgress(nodeID)
		go m.ensureConnection(nodeID)
	}
}

// acceptLoop listens for and handles new inbound TCP connections.
func (m *manager) acceptLoop() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			if m.ctx.Err() != nil {
				m.logger.Info("Listener accept loop shutting down.")
				return // Context was cancelled, normal shutdown.
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

// handleInboundConnection manages the lifecycle of a new connection initiated by a remote peer.
func (m *manager) handleInboundConnection(conn net.Conn) {
	// The remoteNodeID is unknown until after the handshake.
	nodeConn := newNodeConnection(conn, "unknown", m.config.NodeConnectionConfig(), m.logger)

	if err := nodeConn.performHandshake(m.ctx, m.config.LocalNodeID, false); err != nil {
		m.logger.Warn("Inbound handshake failed", zap.Error(err), zap.String("remote_addr", conn.RemoteAddr().String()))
		nodeConn.Close()
		return
	}

	remoteNodeID := nodeConn.RemoteNodeID()

	// Deterministically resolve connection race.
	if m.shouldDropInbound(remoteNodeID) {
		m.logger.Debug("Dropping inbound connection due to race resolution", zap.String("remote_node", string(remoteNodeID)))
		nodeConn.Close()
		return
	}

	// Check if we already have a connection
	existingConn, _ := m.nodeStates.GetNodeConnection(remoteNodeID)
	if existingConn != nil {
		m.logger.Debug("Inbound connection rejected - already connected", zap.String("remote_node", string(remoteNodeID)))
		nodeConn.Close()
		return
	}

	// Accept the inbound connection
	m.nodeStates.SetNodeConnection(remoteNodeID, nodeConn, StateConnected)
	m.retryScheduler.ResetRetry(remoteNodeID)

	m.logger.Info("Accepted inbound connection", zap.String("remote_node", string(remoteNodeID)))

	// Start draining the message queue to the connection
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.drainQueueToConnection(remoteNodeID, nodeConn)
	}()

	// Block here until the connection is terminated.
	nodeConn.Run(m.ctx, m.onMessage)

	// Once Run returns, the connection is dead. Handle failure.
	m.handleConnectionFailure(remoteNodeID, fmt.Errorf("connection terminated"))
}

// shouldDropInbound resolves the "simultaneous dial" race condition.
// To ensure one stable connection, the node with the lexicographically
// smaller ID is responsible for maintaining the connection.
// Therefore, if this node (the "greater" one) receives an inbound connection
// from a "lesser" node, it drops it, trusting that its own outbound dial will succeed.
func (m *manager) shouldDropInbound(remoteNodeID cluster.NodeID) bool {
	return strings.Compare(string(m.config.LocalNodeID), string(remoteNodeID)) > 0
}
