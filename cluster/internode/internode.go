package internode

//
//import (
//	"bufio"
//	"context"
//	"fmt"
//	"io"
//	"net"
//	"strconv"
//	"sync"
//	"time"
//
//	"github.com/ponyruntime/pony/api/cluster"
//	"github.com/ponyruntime/pony/api/event"
//	"github.com/ponyruntime/pony/api/payload"
//	"github.com/ponyruntime/pony/api/pubsub"
//	"github.com/ponyruntime/pony/system/eventbus"
//	"go.uber.org/zap"
//)
//
//const (
//	DefaultInterNodePort = 7950 // Default port for inter-node communication
//	HandshakeTimeout     = 5 * time.Second
//	ReconnectDelay       = 2 * time.Second
//	MaxReconnectAttempts = 3
//)
//
//// Service manages inter-node communication and message routing
//type Service struct {
//	ctx        context.Context
//	cancel     context.CancelFunc
//	logger     *zap.Logger
//	bus        event.Bus
//	dtt        payload.Transcoder
//	membership cluster.Membership
//
//	// Local node info
//	localNodeID string
//	bindAddr    string
//	bindPort    int
//
//	// Connection management
//	connections map[string]*NodeConnection
//	connMutex   sync.RWMutex
//	listener    net.Listener
//
//	// Message handling
//	upstream   chan *pubsub.Package // Upstream messages to route
//	subscriber *eventbus.Subscriber
//
//	// Local node for fallback
//	localNode pubsub.Node
//}
//
//// Config holds service configuration
//type Config struct {
//	LocalNodeID string
//	BindAddr    string
//	BindPort    int // 0 for dynamic allocation
//	Logger      *zap.Logger
//	Bus         event.Bus
//	Transcoder  payload.Transcoder
//	Membership  cluster.Membership
//	LocalNode   pubsub.Node
//}
//
//// NewService creates a new inter-node communication service
//func NewService(config Config) *Service {
//	ctx, cancel := context.WithCancel(context.Background())
//
//	return &Service{
//		ctx:         ctx,
//		cancel:      cancel,
//		logger:      config.Logger,
//		bus:         config.Bus,
//		dtt:         config.Transcoder,
//		membership:  config.Membership,
//		localNodeID: config.LocalNodeID,
//		bindAddr:    config.BindAddr,
//		bindPort:    config.BindPort,
//		connections: make(map[string]*NodeConnection),
//		upstream:    make(chan *pubsub.Package, 1000),
//		localNode:   config.LocalNode,
//	}
//}
//
//// Start initializes the service and begins handling connections
//func (s *Service) Start(ctx context.Context) error {
//	s.logger.Info("starting inter-node communication service",
//		zap.String("local_node", s.localNodeID),
//		zap.String("bind_addr", s.bindAddr))
//
//	// Start TCP listener
//	if err := s.startListener(); err != nil {
//		return fmt.Errorf("failed to start listener: %w", err)
//	}
//
//	// Subscribe to cluster membership events
//	if err := s.subscribeToMembershipEvents(); err != nil {
//		return fmt.Errorf("failed to subscribe to membership events: %w", err)
//	}
//
//	// Start message processing
//	go s.processUpstreamMessages()
//
//	// Connect to existing nodes
//	go s.connectToExistingNodes()
//
//	s.logger.Info("inter-node service started successfully",
//		zap.String("listen_addr", s.listener.Addr().String()))
//
//	return nil
//}
//
//// Stop gracefully shuts down the service
//func (s *Service) Stop() error {
//	s.logger.Info("stopping inter-node service")
//
//	s.cancel()
//
//	// Close listener
//	if s.listener != nil {
//		s.listener.Close()
//	}
//
//	// Close subscriber
//	if s.subscriber != nil {
//		s.subscriber.Close()
//	}
//
//	// Close all connections
//	s.connMutex.Lock()
//	for _, conn := range s.connections {
//		conn.Close()
//	}
//	s.connMutex.Unlock()
//
//	s.logger.Info("inter-node service stopped")
//	return nil
//}
//
//// Send implements pubsub.Receiver interface for upstream routing
//func (s *Service) Send(pkg *pubsub.Package) error {
//	// Route based on target node
//	if pkg.Target.Node == s.localNodeID || pkg.Target.Node == "" {
//		// Local delivery
//		return s.localNode.Send(pkg)
//	}
//
//	// Remote delivery - queue for processing
//	select {
//	case s.upstream <- pkg:
//		return nil
//	default:
//		return fmt.Errorf("upstream queue full")
//	}
//}
//
//// startListener starts the TCP listener for incoming connections
//func (s *Service) startListener() error {
//	// Allocate port dynamically if not specified
//	if s.bindPort == 0 {
//		if err := s.allocateDynamicPort(); err != nil {
//			return err
//		}
//	}
//
//	addr := fmt.Sprintf("%s:%d", s.bindAddr, s.bindPort)
//	listener, err := net.Listen("tcp", addr)
//	if err != nil {
//		return err
//	}
//
//	s.listener = listener
//
//	// Accept incoming connections
//	go s.acceptIncomingConnections()
//
//	return nil
//}
//
//// allocateDynamicPort finds an available port and updates node metadata
//func (s *Service) allocateDynamicPort() error {
//	// Find available port
//	listener, err := net.Listen("tcp", s.bindAddr+":0")
//	if err != nil {
//		return err
//	}
//
//	addr := listener.Addr().(*net.TCPAddr)
//	s.bindPort = addr.Port
//	listener.Close()
//
//	s.logger.Info("allocated dynamic port", zap.Int("port", s.bindPort))
//
//	// TODO: Update node metadata with allocated port
//	// This would require extending your membership service to support metadata updates
//
//	return nil
//}
//
//// acceptIncomingConnections handles incoming TCP connections
//func (s *Service) acceptIncomingConnections() {
//	for {
//		conn, err := s.listener.Accept()
//		if err != nil {
//			if s.ctx.Err() != nil {
//				return // Service is shutting down
//			}
//			s.logger.Error("failed to accept connection", zap.Error(err))
//			continue
//		}
//
//		go s.handleIncomingConnection(conn)
//	}
//}
//
//// handleIncomingConnection processes a new incoming connection
//func (s *Service) handleIncomingConnection(conn net.Conn) {
//	defer conn.Close()
//
//	// Perform handshake to identify remote node
//	remoteNodeID, err := s.performIncomingHandshake(conn)
//	if err != nil {
//		s.logger.Error("handshake failed", zap.Error(err))
//		return
//	}
//
//	// Check for connection race
//	if s.shouldRejectConnection(remoteNodeID) {
//		s.logger.Debug("rejecting connection due to race condition",
//			zap.String("remote_node", remoteNodeID))
//		return
//	}
//
//	// Create and register connection
//	nodeConn := NewNodeConnection(conn, s.localNodeID, remoteNodeID, s.dtt, s.logger)
//	s.registerConnection(remoteNodeID, nodeConn)
//
//	// Handle connection lifecycle
//	nodeConn.HandleConnection(s.ctx, s.onMessageReceived)
//}
//
//// subscribeToMembershipEvents subscribes to cluster membership changes
//func (s *Service) subscribeToMembershipEvents() error {
//	sub, err := eventbus.NewSubscriber(
//		s.ctx,
//		s.bus,
//		cluster.System,
//		"node.(joined|left|updated)",
//		s.handleMembershipEvent,
//	)
//	if err != nil {
//		return err
//	}
//	s.subscriber = sub
//	return nil
//}
//
//// handleMembershipEvent processes cluster membership changes
//func (s *Service) handleMembershipEvent(e event.Event) {
//	nodeEvent, ok := e.Data.(cluster.NodeEvent)
//	if !ok {
//		s.logger.Error("invalid node event data", zap.String("type", fmt.Sprintf("%T", e.Data)))
//		return
//	}
//
//	nodeID := string(nodeEvent.Node.ID)
//
//	switch e.Kind {
//	case cluster.NodeJoinedEventKind:
//		s.handleNodeJoined(nodeEvent.Node)
//	case cluster.NodeLeftEventKind:
//		s.handleNodeLeft(nodeID)
//	case cluster.NodeUpdatedEventKind:
//		s.handleNodeUpdated(nodeEvent.Node)
//	}
//}
//
//// handleNodeJoined establishes connection to newly joined node
//func (s *Service) handleNodeJoined(node cluster.NodeInfo) {
//	nodeID := string(node.ID)
//
//	if s.shouldInitiateConnection(nodeID) {
//		go s.establishOutboundConnection(node)
//	}
//}
//
//// handleNodeLeft cleans up connection to departed node
//func (s *Service) handleNodeLeft(nodeID string) {
//	s.connMutex.Lock()
//	defer s.connMutex.Unlock()
//
//	if conn, exists := s.connections[nodeID]; exists {
//		conn.Close()
//		delete(s.connections, nodeID)
//		s.logger.Info("removed connection to departed node", zap.String("node", nodeID))
//	}
//}
//
//// handleNodeUpdated handles node metadata updates
//func (s *Service) handleNodeUpdated(node cluster.NodeInfo) {
//	// TODO: Handle port changes, etc.
//	s.logger.Debug("node updated", zap.String("node", string(node.ID)))
//}
//
//// shouldInitiateConnection determines if we should connect to a node
//func (s *Service) shouldInitiateConnection(remoteNodeID string) bool {
//	return s.localNodeID < remoteNodeID // Lexicographic ordering
//}
//
//// shouldRejectConnection handles connection race resolution
//func (s *Service) shouldRejectConnection(remoteNodeID string) bool {
//	s.connMutex.RLock()
//	defer s.connMutex.RUnlock()
//
//	// If we already have a connection and we should have initiated it, reject this one
//	if _, exists := s.connections[remoteNodeID]; exists {
//		return s.shouldInitiateConnection(remoteNodeID)
//	}
//
//	return false
//}
//
//// connectToExistingNodes establishes connections to current cluster members
//func (s *Service) connectToExistingNodes() {
//	nodes := s.membership.Nodes()
//	for _, node := range nodes {
//		nodeID := string(node.ID)
//		if nodeID != s.localNodeID && s.shouldInitiateConnection(nodeID) {
//			go s.establishOutboundConnection(node)
//		}
//	}
//}
//
//// establishOutboundConnection creates connection to remote node
//func (s *Service) establishOutboundConnection(node cluster.NodeInfo) {
//	nodeID := string(node.ID)
//
//	// Extract inter-node port from metadata
//	port := s.extractInterNodePort(node)
//	if port == 0 {
//		s.logger.Error("no inter-node port found for node", zap.String("node", nodeID))
//		return
//	}
//
//	addr := fmt.Sprintf("%s:%d", node.Addr, port)
//
//	conn, err := net.DialTimeout("tcp", addr, HandshakeTimeout)
//	if err != nil {
//		s.logger.Error("failed to connect to node",
//			zap.String("node", nodeID),
//			zap.String("addr", addr),
//			zap.Error(err))
//		return
//	}
//
//	// Perform handshake
//	if err := s.performOutgoingHandshake(conn, nodeID); err != nil {
//		conn.Close()
//		s.logger.Error("outgoing handshake failed",
//			zap.String("node", nodeID),
//			zap.Error(err))
//		return
//	}
//
//	// Create and register connection
//	nodeConn := NewNodeConnection(conn, s.localNodeID, nodeID, s.dtt, s.logger)
//	s.registerConnection(nodeID, nodeConn)
//
//	// Handle connection lifecycle
//	nodeConn.HandleConnection(s.ctx, s.onMessageReceived)
//}
//
//// processUpstreamMessages handles queued messages for remote delivery
//func (s *Service) processUpstreamMessages() {
//	for {
//		select {
//		case <-s.ctx.Done():
//			return
//		case pkg := <-s.upstream:
//			s.routeMessage(pkg)
//		}
//	}
//}
//
//// routeMessage sends message to appropriate remote node
//func (s *Service) routeMessage(pkg *pubsub.Package) {
//	s.connMutex.RLock()
//	conn, exists := s.connections[pkg.Target.Node]
//	s.connMutex.RUnlock()
//
//	if !exists {
//		s.logger.Error("no connection to target node",
//			zap.String("target_node", pkg.Target.Node),
//			zap.String("target_pid", pkg.Target.String()))
//		return
//	}
//
//	if err := conn.SendMessage(pkg); err != nil {
//		s.logger.Error("failed to send message",
//			zap.String("target_node", pkg.Target.Node),
//			zap.Error(err))
//	}
//}
//
//// registerConnection adds connection to registry
//func (s *Service) registerConnection(nodeID string, conn *NodeConnection) {
//	s.connMutex.Lock()
//	s.connections[nodeID] = conn
//	s.connMutex.Unlock()
//
//	s.logger.Info("registered connection", zap.String("node", nodeID))
//}
//
//// onMessageReceived handles messages received from remote nodes
//func (s *Service) onMessageReceived(pkg *pubsub.Package) {
//	// Forward to local node
//	if err := s.localNode.Send(pkg); err != nil {
//		s.logger.Error("failed to deliver received message",
//			zap.String("source", pkg.Source.String()),
//			zap.String("target", pkg.Target.String()),
//			zap.Error(err))
//	}
//}
//
//// extractInterNodePort extracts inter-node port from node metadata
//func (s *Service) extractInterNodePort(node cluster.NodeInfo) int {
//	if portStr, exists := node.Meta["internode_port"]; exists {
//		if port, err := strconv.Atoi(portStr); err == nil {
//			return port
//		}
//	}
//	return DefaultInterNodePort // Fallback to default
//}
//
//// Handshake protocol implementation
//func (s *Service) performOutgoingHandshake(conn net.Conn, expectedNodeID string) error {
//	// Send our node ID
//	if err := s.sendNodeID(conn, s.localNodeID); err != nil {
//		return err
//	}
//
//	// Receive their node ID
//	remoteNodeID, err := s.receiveNodeID(conn)
//	if err != nil {
//		return err
//	}
//
//	if remoteNodeID != expectedNodeID {
//		return fmt.Errorf("node ID mismatch: expected %s, got %s", expectedNodeID, remoteNodeID)
//	}
//
//	return nil
//}
//
//func (s *Service) performIncomingHandshake(conn net.Conn) (string, error) {
//	// Receive their node ID
//	remoteNodeID, err := s.receiveNodeID(conn)
//	if err != nil {
//		return "", err
//	}
//
//	// Send our node ID
//	if err := s.sendNodeID(conn, s.localNodeID); err != nil {
//		return "", err
//	}
//
//	return remoteNodeID, nil
//}
//
//func (s *Service) sendNodeID(conn net.Conn, nodeID string) error {
//	data := []byte(nodeID)
//	return writeNetstring(conn, data)
//}
//
//func (s *Service) receiveNodeID(conn net.Conn) (string, error) {
//	data, err := readNetstring(conn)
//	if err != nil {
//		return "", err
//	}
//	return string(data), nil
//}
//
//// Netstring protocol implementation (goridge-like)
//func writeNetstring(w io.Writer, data []byte) error {
//	length := len(data)
//	lengthStr := strconv.Itoa(length)
//
//	// Write length + ':' + data + ','
//	if _, err := w.Write([]byte(lengthStr)); err != nil {
//		return err
//	}
//	if _, err := w.Write([]byte{':'}); err != nil {
//		return err
//	}
//	if _, err := w.Write(data); err != nil {
//		return err
//	}
//	if _, err := w.Write([]byte{','}); err != nil {
//		return err
//	}
//
//	return nil
//}
//
//func readNetstring(r io.Reader) ([]byte, error) {
//	reader := bufio.NewReader(r)
//
//	// Read length until ':'
//	lengthStr, err := reader.ReadString(':')
//	if err != nil {
//		return nil, err
//	}
//
//	// Parse length
//	lengthStr = lengthStr[:len(lengthStr)-1] // Remove ':'
//	length, err := strconv.Atoi(lengthStr)
//	if err != nil {
//		return nil, err
//	}
//
//	// Read data
//	data := make([]byte, length)
//	if _, err := io.ReadFull(reader, data); err != nil {
//		return nil, err
//	}
//
//	// Read trailing ','
//	trailer, err := reader.ReadByte()
//	if err != nil {
//		return nil, err
//	}
//	if trailer != ',' {
//		return nil, fmt.Errorf("invalid netstring trailer: expected ',', got %c", trailer)
//	}
//
//	return data, nil
//}
//
//// NodeConnection represents a connection to a remote node
//type NodeConnection struct {
//	conn         net.Conn
//	localNodeID  string
//	remoteNodeID string
//	dtt          payload.Transcoder
//	logger       *zap.Logger
//
//	// Channels for async communication
//	outbound chan *pubsub.Package
//	closed   chan struct{}
//
//	// Connection state
//	mu     sync.RWMutex
//	active bool
//}
//
//// NewNodeConnection creates a new node connection
//func NewNodeConnection(conn net.Conn, localNodeID, remoteNodeID string, dtt payload.Transcoder, logger *zap.Logger) *NodeConnection {
//	return &NodeConnection{
//		conn:         conn,
//		localNodeID:  localNodeID,
//		remoteNodeID: remoteNodeID,
//		dtt:          dtt,
//		logger:       logger.With(zap.String("remote_node", remoteNodeID)),
//		outbound:     make(chan *pubsub.Package, 100),
//		closed:       make(chan struct{}),
//		active:       true,
//	}
//}
//
//// HandleConnection manages the connection lifecycle
//func (nc *NodeConnection) HandleConnection(ctx context.Context, onMessage func(*pubsub.Package)) {
//	defer nc.cleanup()
//
//	// Start reader goroutine
//	go nc.readMessages(ctx, onMessage)
//
//	// Start writer goroutine
//	go nc.writeMessages(ctx)
//
//	// Wait for context cancellation or connection closure
//	select {
//	case <-ctx.Done():
//		nc.logger.Debug("connection closed due to context cancellation")
//	case <-nc.closed:
//		nc.logger.Debug("connection closed")
//	}
//}
//
//// SendMessage queues a message for sending
//func (nc *NodeConnection) SendMessage(pkg *pubsub.Package) error {
//	nc.mu.RLock()
//	defer nc.mu.RUnlock()
//
//	if !nc.active {
//		return fmt.Errorf("connection to %s is closed", nc.remoteNodeID)
//	}
//
//	select {
//	case nc.outbound <- pkg:
//		return nil
//	default:
//		return fmt.Errorf("outbound queue full for node %s", nc.remoteNodeID)
//	}
//}
//
//// Close closes the connection
//func (nc *NodeConnection) Close() {
//	nc.mu.Lock()
//	defer nc.mu.Unlock()
//
//	if nc.active {
//		nc.active = false
//		close(nc.closed)
//		nc.conn.Close()
//	}
//}
//
//// readMessages handles incoming messages
//func (nc *NodeConnection) readMessages(ctx context.Context, onMessage func(*pubsub.Package)) {
//	for {
//		select {
//		case <-ctx.Done():
//			return
//		case <-nc.closed:
//			return
//		default:
//			// Read message from connection
//			data, err := readNetstring(nc.conn)
//			if err != nil {
//				nc.logger.Error("failed to read message", zap.Error(err))
//				nc.Close()
//				return
//			}
//
//			// Deserialize message
//			var pkg pubsub.Package
//			if err := nc.dtt.Unmarshal(data, &pkg); err != nil {
//				nc.logger.Error("failed to unmarshal message", zap.Error(err))
//				continue
//			}
//
//			// Handle message
//			onMessage(&pkg)
//		}
//	}
//}
//
//// writeMessages handles outgoing messages
//func (nc *NodeConnection) writeMessages(ctx context.Context) {
//	for {
//		select {
//		case <-ctx.Done():
//			return
//		case <-nc.closed:
//			return
//		case pkg := <-nc.outbound:
//			// Serialize message
//			data, err := nc.dtt.Marshal(pkg)
//			if err != nil {
//				nc.logger.Error("failed to marshal message", zap.Error(err))
//				continue
//			}
//
//			// Send message
//			if err := writeNetstring(nc.conn, data); err != nil {
//				nc.logger.Error("failed to write message", zap.Error(err))
//				nc.Close()
//				return
//			}
//		}
//	}
//}
//
//// cleanup performs connection cleanup
//func (nc *NodeConnection) cleanup() {
//	nc.Close()
//	nc.logger.Debug("connection cleanup completed")
//}
