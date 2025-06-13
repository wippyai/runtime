package internode

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Config holds connection manager configuration
type Config struct {
	LocalNodeID          string
	BindAddr             string
	BindPort             int
	HandshakeTimeout     time.Duration
	ReconnectDelay       time.Duration
	MaxReconnectAttempts int
	OutboundQueueSize    int
	Logger               *zap.Logger
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		HandshakeTimeout:     5 * time.Second,
		ReconnectDelay:       2 * time.Second,
		MaxReconnectAttempts: 3,
		OutboundQueueSize:    100,
	}
}

// Manager handles inter-node TCP connections
type Manager interface {
	Start(ctx context.Context, onMessage func(nodeID string, data []byte)) error
	Stop() error
	SendToNode(nodeID string, data []byte) error
	ConnectToNode(nodeID string, addr string, port int) error
	DisconnectFromNode(nodeID string)
	GetConnectedNodes() []string
}

// manager implementation
type manager struct {
	config Config
	ctx    context.Context
	cancel context.CancelFunc
	logger *zap.Logger
	wg     sync.WaitGroup

	// TCP management
	listener    net.Listener
	connections map[string]*NodeConnection
	connMutex   sync.RWMutex

	// Callback for upstream
	onMessage func(string, []byte)
}

// NewManager creates a new connection manager
func NewManager(config Config) Manager {
	return &manager{
		config:      config,
		logger:      config.Logger,
		connections: make(map[string]*NodeConnection),
	}
}

// Start initializes the connection manager and begins listening
func (m *manager) Start(ctx context.Context, onMessage func(nodeID string, data []byte)) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.onMessage = onMessage

	m.logger.Info("starting connection manager",
		zap.String("local_node", m.config.LocalNodeID),
		zap.String("bind_addr", fmt.Sprintf("%s:%d", m.config.BindAddr, m.config.BindPort)))

	// Start TCP listener
	if err := m.startListener(); err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	m.logger.Info("connection manager started successfully",
		zap.String("listen_addr", m.listener.Addr().String()))

	return nil
}

// Stop gracefully shuts down the connection manager
func (m *manager) Stop() error {
	m.logger.Info("stopping connection manager")

	m.cancel()

	// Close listener
	if m.listener != nil {
		m.listener.Close()
	}

	// Close all connections
	m.connMutex.Lock()
	for _, conn := range m.connections {
		conn.Close()
	}
	m.connMutex.Unlock()

	// Wait for goroutines
	m.wg.Wait()

	m.logger.Info("connection manager stopped")
	return nil
}

// SendToNode sends data to a specific node
func (m *manager) SendToNode(nodeID string, data []byte) error {
	m.connMutex.RLock()
	conn, exists := m.connections[nodeID]
	m.connMutex.RUnlock()

	if !exists {
		return fmt.Errorf("no connection to node %s", nodeID)
	}

	return conn.Send(data)
}

// ConnectToNode establishes outbound connection to a node
func (m *manager) ConnectToNode(nodeID string, addr string, port int) error {
	// Check if already connected
	m.connMutex.RLock()
	if _, exists := m.connections[nodeID]; exists {
		m.connMutex.RUnlock()
		return nil // Already connected
	}
	m.connMutex.RUnlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.establishOutboundConnection(nodeID, addr, port)
	}()

	return nil
}

// DisconnectFromNode closes connection to a node
func (m *manager) DisconnectFromNode(nodeID string) {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()

	if conn, exists := m.connections[nodeID]; exists {
		conn.Close()
		delete(m.connections, nodeID)
		m.logger.Info("disconnected from node", zap.String("node", nodeID))
	}
}

// GetConnectedNodes returns list of currently connected node IDs
func (m *manager) GetConnectedNodes() []string {
	m.connMutex.RLock()
	defer m.connMutex.RUnlock()

	nodes := make([]string, 0, len(m.connections))
	for nodeID := range m.connections {
		nodes = append(nodes, nodeID)
	}
	return nodes
}

// startListener starts the TCP listener for incoming connections
func (m *manager) startListener() error {
	addr := fmt.Sprintf("%s:%d", m.config.BindAddr, m.config.BindPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	m.listener = listener

	// Accept incoming connections
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.acceptLoop()
	}()

	return nil
}

// acceptLoop handles incoming TCP connections
func (m *manager) acceptLoop() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			if m.ctx.Err() != nil {
				return // Service is shutting down
			}
			m.logger.Error("failed to accept connection", zap.Error(err))
			continue
		}

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.handleInboundConnection(conn)
		}()
	}
}

// handleInboundConnection processes a new incoming connection
func (m *manager) handleInboundConnection(conn net.Conn) {
	defer conn.Close()

	// Create connection wrapper
	nodeConn := NewNodeConnection(conn, m.config.LocalNodeID, "", m.config, m.logger)

	// Perform handshake
	if err := nodeConn.PerformHandshake(false, ""); err != nil {
		m.logger.Error("inbound handshake failed", zap.Error(err))
		return
	}

	remoteNodeID := nodeConn.remoteNodeID

	// Check for connection race - use lexicographic ordering
	if m.shouldRejectConnection(remoteNodeID) {
		m.logger.Debug("rejecting connection due to race condition",
			zap.String("remote_node", remoteNodeID))
		return
	}

	// Register connection
	if !m.registerConnection(remoteNodeID, nodeConn) {
		m.logger.Debug("connection already exists",
			zap.String("remote_node", remoteNodeID))
		return
	}

	// Start connection handling
	nodeConn.Start(m.ctx, func(nodeID string, data []byte) {
		m.onMessage(nodeID, data)
		// Remove connection on completion
		m.unregisterConnection(nodeID)
	})
}

// establishOutboundConnection creates connection to remote node
func (m *manager) establishOutboundConnection(nodeID string, addr string, port int) {
	fullAddr := fmt.Sprintf("%s:%d", addr, port)

	conn, err := net.DialTimeout("tcp", fullAddr, m.config.HandshakeTimeout)
	if err != nil {
		m.logger.Error("failed to connect to node",
			zap.String("node", nodeID),
			zap.String("addr", fullAddr),
			zap.Error(err))
		return
	}

	// Create connection wrapper
	nodeConn := NewNodeConnection(conn, m.config.LocalNodeID, nodeID, m.config, m.logger)

	// Perform handshake
	if err := nodeConn.PerformHandshake(true, nodeID); err != nil {
		conn.Close()
		m.logger.Error("outbound handshake failed",
			zap.String("node", nodeID),
			zap.Error(err))
		return
	}

	// Register connection
	if !m.registerConnection(nodeID, nodeConn) {
		conn.Close()
		m.logger.Debug("connection already exists",
			zap.String("remote_node", nodeID))
		return
	}

	// Start connection handling
	nodeConn.Start(m.ctx, func(nodeID string, data []byte) {
		m.onMessage(nodeID, data)
		// Remove connection on completion
		m.unregisterConnection(nodeID)
	})
}

// shouldRejectConnection handles connection race resolution using lexicographic ordering
func (m *manager) shouldRejectConnection(remoteNodeID string) bool {
	m.connMutex.RLock()
	defer m.connMutex.RUnlock()

	// If we already have a connection and we should have initiated it, reject this one
	if _, exists := m.connections[remoteNodeID]; exists {
		return m.config.LocalNodeID < remoteNodeID
	}

	return false
}

// registerConnection adds connection to registry
func (m *manager) registerConnection(nodeID string, conn *NodeConnection) bool {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()

	// Double-check that connection doesn't already exist
	if _, exists := m.connections[nodeID]; exists {
		return false
	}

	m.connections[nodeID] = conn
	m.logger.Info("registered connection", zap.String("node", nodeID))
	return true
}

// unregisterConnection removes connection from registry
func (m *manager) unregisterConnection(nodeID string) {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()

	if _, exists := m.connections[nodeID]; exists {
		delete(m.connections, nodeID)
		m.logger.Info("unregistered connection", zap.String("node", nodeID))
	}
}
