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

	// Track if we initiated the connection
	isOutbound bool
}

type ConnectionManager interface {
	Start(ctx context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) error
	Stop() error
	SendToNode(nodeID cluster.NodeID, data []byte) error
	EnsureConnection(nodeID cluster.NodeID, addr string, port int)
	DisconnectFromNode(nodeID cluster.NodeID)
	ConnectedNodes() []cluster.NodeID
	HandleNodeLeft(nodeID cluster.NodeID)
	GetListenPort() int
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
		m.logger.Info("TLS encryption enabled for inter-node communication")
	}

	listener, actualPort, err := m.startListener()
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	m.listener = listener
	m.actualPort = actualPort

	m.logger.Info("TCP listener started",
		zap.String("address", fmt.Sprintf("%s:%d", m.config.BindAddr, actualPort)),
		zap.Bool("auto_port", m.config.AutoPort),
		zap.Int("configured_port", m.config.BindPort))

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
		if err := m.listener.Close(); err != nil {
			m.logger.Error("Error closing listener", zap.Error(err))
		}
	}

	m.controlLoopsMu.Lock()
	for nodeID, loop := range m.controlLoops {
		m.logger.Debug("Stopping control loop", zap.String("node", string(nodeID)))
		select {
		case loop.commands <- nodeCommand{Type: cmdKill}:
		default:
			loop.cancel()
		}
	}
	m.controlLoopsMu.Unlock()

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Info("All goroutines stopped gracefully")
	case <-time.After(10 * time.Second):
		m.logger.Warn("Shutdown timeout reached, some goroutines may not have stopped")
	}

	m.logger.Info("Connection manager stopped")
	return nil
}

func (m *manager) SendToNode(nodeID cluster.NodeID, data []byte) error {
	m.nodeStates.QueueMessage(nodeID, data)
	return nil
}

func (m *manager) EnsureConnection(nodeID cluster.NodeID, addr string, port int) {
	// Check if we already have a connection
	_, currentState := m.nodeStates.GetNodeConnection(nodeID)
	if currentState == StateConnected {
		return
	}

	// Only initiate outbound if we should (based on tie-breaking)
	if !m.shouldInitiateConnection(nodeID) {
		m.logger.Debug("Tie-break: not initiating connection", zap.String("node", string(nodeID)))
		return
	}

	m.logger.Debug("Ensuring connection", zap.String("node", string(nodeID)), zap.String("addr", fmt.Sprintf("%s:%d", addr, port)))
	m.nodeStates.UpdateNodeAddress(nodeID, addr, port)
	m.sendCommand(nodeID, nodeCommand{
		Type: cmdConnect,
		Data: connectData{Addr: addr, Port: port},
	})
}

func (m *manager) DisconnectFromNode(nodeID cluster.NodeID) {
	m.logger.Debug("Disconnecting from node", zap.String("node", string(nodeID)))
	m.sendCommand(nodeID, nodeCommand{Type: cmdKill})
}

func (m *manager) ConnectedNodes() []cluster.NodeID {
	return m.nodeStates.GetConnectedNodes()
}

func (m *manager) HandleNodeLeft(nodeID cluster.NodeID) {
	m.logger.Info("Node left cluster", zap.String("node", string(nodeID)))
	m.sendCommand(nodeID, nodeCommand{Type: cmdKill})
	m.nodeStates.RemoveNode(nodeID)
}

func (m *manager) GetListenPort() int {
	return m.actualPort
}

func (m *manager) sendCommand(nodeID cluster.NodeID, cmd nodeCommand) {
	m.controlLoopsMu.Lock()
	loop, exists := m.controlLoops[nodeID]

	if !exists {
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
		m.logger.Debug("Created control loop", zap.String("node", string(nodeID)))

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
	case <-m.ctx.Done():
		m.logger.Debug("Command dropped due to system shutdown", zap.String("node", string(nodeID)))
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

	messageNotifier := loop.manager.nodeStates.GetMessageNotifier(loop.nodeID)
	if messageNotifier == nil {
		messageNotifier = make(<-chan struct{})
	}

	for {
		select {
		case <-loop.ctx.Done():
			loop.cleanup()
			return

		case cmd := <-loop.commands:
			shouldExit := loop.handleCommand(cmd)
			if shouldExit {
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
	loop.logger.Debug("Processing command",
		zap.String("command", fmt.Sprintf("%d", cmd.Type)),
		zap.String("current_state", loop.state.String()))

	switch cmd.Type {
	case cmdConnect:
		data, ok := cmd.Data.(connectData)
		if !ok {
			loop.logger.Error("Invalid connect command data type")
			return false
		}
		loop.handleConnect(data)

	case cmdConnected:
		data, ok := cmd.Data.(connectedData)
		if !ok {
			loop.logger.Error("Invalid connected command data type")
			return false
		}
		loop.handleConnected(data)

	case cmdDisconnected:
		data, ok := cmd.Data.(disconnectedData)
		if !ok {
			loop.logger.Error("Invalid disconnected command data type")
			return false
		}
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

	// Don't connect if already connected or connecting
	if loop.state == StateConnecting || loop.state == StateConnected {
		return
	}

	if loop.state == StateDead {
		return
	}

	loop.logger.Debug("Initiating connection", zap.String("addr", fmt.Sprintf("%s:%d", data.Addr, data.Port)))
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
	// If we're already connected, reject the new connection
	if loop.state == StateConnected {
		loop.logger.Debug("Already connected, rejecting new connection")
		if data.Connection != nil {
			data.Connection.Close()
		}
		return
	}

	// Only accept connections in appropriate states
	if loop.state != StateConnecting && loop.state != StateNone && loop.state != StateRetrying {
		loop.logger.Debug("Unexpected state for new connection, closing", zap.String("state", loop.state.String()))
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

	loop.logger.Info("Connection established")

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
		pendingMessages := loop.connection.ExtractPendingMessages()
		loop.connection.Close()
		loop.connection = nil

		if len(pendingMessages) > 0 {
			loop.manager.nodeStates.RequeueMessages(loop.nodeID, pendingMessages)
		}
	}

	loop.manager.nodeStates.SetNodeConnection(loop.nodeID, nil, StateNone)

	// Only retry if we initiated the connection and should retry
	if data.ShouldRetry && loop.isOutbound && loop.retryCount < loop.manager.config.MaxRetryAttempts {
		loop.state = StateRetrying
		loop.retryCount++

		if loop.retryDelay < loop.manager.config.MaxRetryDelay {
			loop.retryDelay *= 2
		}

		loop.logger.Debug("Scheduling retry",
			zap.Duration("delay", loop.retryDelay),
			zap.Int("attempt", loop.retryCount))

		loop.manager.wg.Add(1)
		go func() {
			defer loop.manager.wg.Done()

			timer := time.NewTimer(loop.retryDelay)
			defer timer.Stop()

			select {
			case <-timer.C:
				addr, port, hasAddr := loop.manager.nodeStates.GetNodeAddress(loop.nodeID)
				if !hasAddr {
					addr, port = loop.addr, loop.port
				}

				loop.sendCommandToSelf(nodeCommand{
					Type: cmdConnect,
					Data: connectData{Addr: addr, Port: port},
				})
			case <-loop.ctx.Done():
			}
		}()
	} else {
		loop.state = StateNone
		if data.Error != nil {
			loop.logger.Debug("Connection failed", zap.Error(data.Error))
		}
	}
}

func (loop *nodeControlLoop) handleKill() {
	loop.logger.Debug("Control loop received kill command")
	loop.state = StateDead
}

func (loop *nodeControlLoop) cleanup() {
	if loop.connection != nil {
		pendingMessages := loop.connection.ExtractPendingMessages()
		loop.connection.Close()
		loop.connection = nil

		if len(pendingMessages) > 0 {
			loop.manager.nodeStates.RequeueMessages(loop.nodeID, pendingMessages)
		}
	}
	loop.manager.nodeStates.SetNodeConnection(loop.nodeID, nil, StateNone)
}

func (loop *nodeControlLoop) sendCommandToSelf(cmd nodeCommand) {
	select {
	case loop.commands <- cmd:
	case <-loop.ctx.Done():
		loop.logger.Debug("Command to self dropped due to context cancellation")
	}
}

func (loop *nodeControlLoop) attemptConnection() {
	if loop.ctx.Err() != nil {
		return
	}

	addr := fmt.Sprintf("%s:%d", loop.addr, loop.port)

	loop.logger.Debug("Attempting outbound connection", zap.String("address", addr))

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

	loop.sendCommandToSelf(nodeCommand{
		Type: cmdConnected,
		Data: connectedData{Connection: nodeConn},
	})
}

func (loop *nodeControlLoop) monitorConnection() {
	if loop.connection == nil {
		return
	}

	handler := func(msg []byte) {
		loop.manager.onMessage(loop.nodeID, msg)
	}

	err := loop.connection.Run(handler)

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

	var unsentMessages [][]byte
	for i, data := range messages {
		if err := loop.connection.Send(data); err != nil {
			unsentMessages = messages[i:]
			break
		}
	}

	if len(unsentMessages) > 0 {
		loop.manager.nodeStates.RequeueMessages(loop.nodeID, unsentMessages)
	}
}

func (loop *nodeControlLoop) sendDisconnected(err error, shouldRetry bool) {
	loop.sendCommandToSelf(nodeCommand{
		Type: cmdDisconnected,
		Data: disconnectedData{Error: err, ShouldRetry: shouldRetry},
	})
}

func (m *manager) startListener() (net.Listener, int, error) {
	if m.config.AutoPort {
		return m.tryPortRange()
	}

	addr := fmt.Sprintf("%s:%d", m.config.BindAddr, m.config.BindPort)
	listener, err := m.listen(addr)
	if err != nil {
		return nil, 0, err
	}

	return listener, m.config.BindPort, nil
}

func (m *manager) tryPortRange() (net.Listener, int, error) {
	startPort := m.config.BindPort
	if startPort == 0 {
		startPort = DefaultPortRangeStart
	}

	if startPort >= DefaultPortRangeStart && startPort <= DefaultPortRangeEnd {
		addr := fmt.Sprintf("%s:%d", m.config.BindAddr, startPort)
		if listener, err := m.listen(addr); err == nil {
			m.logger.Debug("Using configured port", zap.Int("port", startPort))
			return listener, startPort, nil
		}
	}

	for port := DefaultPortRangeStart; port <= DefaultPortRangeEnd; port++ {
		if port == startPort {
			continue
		}

		addr := fmt.Sprintf("%s:%d", m.config.BindAddr, port)
		listener, err := m.listen(addr)
		if err == nil {
			m.logger.Info("Auto-selected port from range",
				zap.Int("port", port),
				zap.Int("range_start", DefaultPortRangeStart),
				zap.Int("range_end", DefaultPortRangeEnd))
			return listener, port, nil
		}
	}

	m.logger.Info("All ports in range busy, using system auto-selection",
		zap.Int("range_start", DefaultPortRangeStart),
		zap.Int("range_end", DefaultPortRangeEnd))

	autoAddr := fmt.Sprintf("%s:0", m.config.BindAddr)
	listener, err := m.listen(autoAddr)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to auto-select port: %w", err)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	m.logger.Info("System auto-selected port", zap.Int("port", actualPort))

	return listener, actualPort, nil
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

		m.logger.Debug("Accepted inbound connection", zap.String("remote_addr", conn.RemoteAddr().String()))

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

	// Check if we already have a connection
	_, currentState := m.nodeStates.GetNodeConnection(remoteNodeID)
	if currentState == StateConnected {
		m.logger.Debug("Already connected, dropping new inbound connection", zap.String("node", string(remoteNodeID)))
		nodeConn.Close()
		return
	}

	shouldDrop := m.shouldDropInbound(remoteNodeID)
	if shouldDrop {
		m.logger.Debug("Dropping inbound connection due to tie-breaking", zap.String("node", string(remoteNodeID)))
		nodeConn.Close()
		return
	}

	m.logger.Debug("Accepting inbound connection", zap.String("node", string(remoteNodeID)))
	m.sendCommand(remoteNodeID, nodeCommand{
		Type: cmdConnected,
		Data: connectedData{Connection: nodeConn},
	})
}

// shouldInitiateConnection determines if we should initiate outbound connection
func (m *manager) shouldInitiateConnection(remoteNodeID cluster.NodeID) bool {
	// Lower node ID initiates the connection
	return strings.Compare(string(m.config.LocalNodeID), string(remoteNodeID)) < 0
}

func (m *manager) shouldDropInbound(remoteNodeID cluster.NodeID) bool {
	// Accept inbound only if we're not supposed to initiate
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
