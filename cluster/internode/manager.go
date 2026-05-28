// SPDX-License-Identifier: MPL-2.0

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
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/metrics"
	"go.uber.org/zap"
)

// DefaultPortRangeStart defines the default start port for internode communication
const (
	DefaultPortRangeStart = 7950
	DefaultPortRangeEnd   = 7959
)

type ConnectionState int

// StateNone represents no connection state
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
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	CAFile   string `json:"ca_file"`
	Enabled  bool   `json:"enabled"`
}

type ManagerConfig struct {
	Logger            *zap.Logger
	BindAddr          string
	LocalNodeID       cluster.NodeID
	TLS               ManagerTLSConfig
	MaxRetryAttempts  int
	BindPort          int
	CommandQueueSize  int
	HandshakeTimeout  time.Duration
	OutboundQueueSize int
	// Per-class send queue caps. Capacities chosen from canonical references:
	// etcd default for control (4096); 2x memberlist default for gossip
	// (1024); sized for fan-out for app broadcasts (2048). Hitting a cap
	// drops with a metric (internode_dropped_total{class,reason="queue_full"})
	// rather than blocking or growing.
	RaftControlQueueCap int
	GossipQueueCap      int
	PGBroadcastQueueCap int
	// RaftMeshQueueCap caps the per-peer queue for the multiplexed Raft
	// transport byte-stream (ClassRaftMesh). Sized like RaftControl: under
	// healthy steady state Raft frames drain fast. On overflow the queue
	// is cleared and the peer's mesh session is reset (drop-oldest is
	// unsafe for a byte-stream) so yamux+raft resync and pre-vote recovers.
	RaftMeshQueueCap int
	// OutboundConnQueueCap caps the per-connection outbound queue inside
	// NodeConnection (drain target of the per-class queues above). Without
	// this cap, network-delay chaos lets the connection-level queue grow
	// unbounded even though the upstream class queues are bounded.
	// Drops here count as internode_dropped_total{reason="conn_queue_full"}.
	OutboundConnQueueCap int
	InitialRetryDelay    time.Duration
	MaxRetryDelay        time.Duration
	DrainBatchSize       int
	MaxMessageSize       uint32
	AutoPort             bool
}

func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		HandshakeTimeout:     5 * time.Second,
		OutboundQueueSize:    256,
		MaxMessageSize:       512 * 1024 * 1024,
		TLS:                  ManagerTLSConfig{Enabled: false},
		InitialRetryDelay:    10 * time.Millisecond,
		MaxRetryDelay:        5 * time.Second,
		AutoPort:             true,
		BindPort:             DefaultPortRangeStart,
		DrainBatchSize:       32,
		CommandQueueSize:     256,
		MaxRetryAttempts:     10,
		RaftControlQueueCap:  4096,
		GossipQueueCap:       1024,
		PGBroadcastQueueCap:  2048,
		RaftMeshQueueCap:     4096,
		OutboundConnQueueCap: 4096,
	}
}

func (mc ManagerConfig) NodeConnectionConfig() NodeConnectionConfig {
	return NodeConnectionConfig{
		HandshakeTimeout: mc.HandshakeTimeout,
		MaxMessageSize:   mc.MaxMessageSize,
		MaxQueueSize:     mc.OutboundConnQueueCap,
	}
}

type nodeCommand struct {
	Data any
	Type commandType
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
	ctx        context.Context
	manager    *manager
	commands   chan nodeCommand
	connection *NodeConnection
	logger     *zap.Logger
	cancel     context.CancelFunc
	nodeID     cluster.NodeID
	addr       string
	state      ConnectionState
	retryDelay time.Duration
	retryCount int
	port       int
	isOutbound bool
}

type ConnectionManager interface {
	Start(ctx context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) error
	Stop() error
	SendToNode(nodeID cluster.NodeID, data []byte, class Class) error
	EnsureConnection(nodeID cluster.NodeID, addr string, port int)
	DisconnectFromNode(nodeID cluster.NodeID)
	ConnectedNodes() []cluster.NodeID
	GetListenPort() int

	// AddManagedNode adds a node to be managed by lifecycle events
	AddManagedNode(nodeID cluster.NodeID)
	RemoveManagedNode(nodeID cluster.NodeID)
	IsManaged(nodeID cluster.NodeID) bool

	// EvictOrphanNodes removes managed nodes that are not present in the
	// supplied authoritative set. Returns the count of evicted nodes.
	// Defensive sweep against the case where a `cluster.NodeLeft` event
	// is missed under partition / gossip storm — without this, the
	// per-node state in the connection manager and its underlying
	// state_manager grows monotonically as the cluster churns.
	EvictOrphanNodes(known map[cluster.NodeID]struct{}) int

	// RecordDropReason increments internode_dropped_total{reason=...} for
	// drop events that originate outside the per-class queue path
	// (RX-side delivery failure, TX-side encode failure, etc.). Lets
	// callers count drops without taking a dependency on the unexported
	// telemetry type.
	RecordDropReason(reason string)

	// RegisterClassReceiver wires an inbound dispatcher for a single
	// sub-protocol class. When a frame with this class arrives, the
	// per-class receiver is invoked instead of the default onMessage
	// callback registered via Start. Returns false if a receiver is
	// already registered for the class. The mesh-backed Raft transport
	// uses this to claim ClassRaftMesh; the default class set continues
	// to flow through Start's onMessage.
	RegisterClassReceiver(class Class, recv func(nodeID cluster.NodeID, data []byte)) bool

	// RegisterClassOverflowHandler wires a callback invoked when the
	// per-peer send queue for `class` overflows and the class's drop
	// policy is "reset the underlying transport" rather than drop a
	// frame. Only ClassRaftMesh uses this: its queue carries a
	// yamux byte-stream, so on overflow the handler tears down the
	// peer's mesh session (close + rebuild) instead of dropping a
	// frame from the middle of the stream. The handler is invoked off
	// the producer's goroutine and MUST NOT block. Returns false if a
	// handler is already registered for the class.
	RegisterClassOverflowHandler(class Class, handler func(nodeID cluster.NodeID)) bool
}

type manager struct {
	ctx              context.Context
	listener         net.Listener
	cancel           context.CancelFunc
	logger           *zap.Logger
	onMessage        func(cluster.NodeID, []byte)
	tlsConfig        *tls.Config
	nodeStates       *NodeStateManager
	controlLoops     map[cluster.NodeID]*nodeControlLoop
	// classReceivers and classOverflow are accessed on every inbound
	// frame (lookupClassReceiver runs in the read hot path). Registrations
	// happen only at boot, so we keep the arrays behind atomic.Pointer
	// snapshots: lookups are an atomic.Load (no mutex), and registrations
	// publish a new array via atomic.Store under a write-only register
	// mutex that serializes the read-modify-write.
	classReceivers atomic.Pointer[[numClasses]func(cluster.NodeID, []byte)]
	classOverflow  atomic.Pointer[[numClasses]func(cluster.NodeID)]
	config         ManagerConfig
	wg             sync.WaitGroup
	actualPort     int
	controlLoopsMu sync.Mutex
	registerMu     sync.Mutex
}

func NewConnectionManager(config ManagerConfig, coll metrics.Collector) ConnectionManager {
	logger := config.Logger.Named("conn")
	tel := newTelemetry(coll)
	return &manager{
		config:       config,
		logger:       logger,
		nodeStates:   NewNodeStateManager(config, tel, logger),
		controlLoops: make(map[cluster.NodeID]*nodeControlLoop),
	}
}

func (m *manager) Start(ctx context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.onMessage = onMessage

	if m.config.TLS.Enabled {
		tlsConfig, err := loadTLSConfig(m.config.TLS)
		if err != nil {
			return NewLoadTLSError(err)
		}
		m.tlsConfig = tlsConfig
	}

	listener, actualPort, err := m.startListener()
	if err != nil {
		return NewStartListenerError(err)
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

func (m *manager) SendToNode(nodeID cluster.NodeID, data []byte, class Class) error {
	err := m.nodeStates.QueueMessageClass(nodeID, data, class)
	if err != nil {
		if errors.Is(err, ErrNodeNotManaged) {
			// Hot path under partition: gossip can mark a peer dead before
			// the PG layer stops targeting it. Counted as a drop with no
			// log to avoid the kind of flood we saw during chaos (thousands
			// per second per pod). The metric is the source of truth.
			m.nodeStates.tel.recordDrop(class, "node_not_managed")
			return nil
		}
		if errors.Is(err, ErrRaftMeshOverflow) {
			// Byte-stream overflow: the queue already discarded the pending
			// stream and recorded drops. Reset the peer's mesh session so
			// yamux+raft re-establish cleanly. Run off this goroutine so a
			// reset never blocks the producer (the handler closes a session;
			// the rebuild happens lazily on the next frame).
			if h := m.lookupClassOverflow(class); h != nil {
				go h(nodeID)
			}
			return nil
		}
		// ErrQueueFull surfaces to the caller (broadcast path will count it).
		return err
	}
	return nil
}

func (m *manager) EnsureConnection(nodeID cluster.NodeID, addr string, port int) {
	if m.nodeStates.GetNodeState(nodeID) == nil {
		m.logger.Error("EnsureConnection called for an unmanaged node. This is a logic error.", zap.String("node", nodeID))
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
	m.logger.Info("Adding new managed node", zap.String("node", nodeID))

	// If the node already has state, it was either:
	// (a) auto-managed by handleInboundConnection (possibly with an active
	//     or in-flight connection), or
	// (b) a stale entry left behind somehow.
	//
	// In case (a) we must not tear down the control loop because it may hold
	// a healthy connection or have a cmdConnected in flight. In case (b) the
	// state is already clean (RemoveManagedNode would have been called on
	// node leave). Either way, skipping teardown is safe.
	if m.nodeStates.GetNodeState(nodeID) != nil {
		m.logger.Debug("Node already managed, skipping teardown",
			zap.String("node", nodeID))
		return
	}

	// If there's an old control loop for this node (e.g. from a previous
	// incarnation that left and is now rejoining), tear it down first to
	// prevent the old loop from interfering with the new state.
	m.controlLoopsMu.Lock()
	if oldLoop, exists := m.controlLoops[nodeID]; exists {
		m.logger.Info("Tearing down stale control loop for rejoining node", zap.String("node", nodeID))
		oldLoop.cancel()
		delete(m.controlLoops, nodeID)
	}
	m.controlLoopsMu.Unlock()

	m.nodeStates.CreateNodeState(nodeID)
}

func (m *manager) RemoveManagedNode(nodeID cluster.NodeID) {
	m.logger.Info("Removing managed node", zap.String("node", nodeID))

	// Cancel the control loop directly and remove it from the map.
	// This is synchronous with respect to the map entry, preventing
	// races where a new node with the same ID starts before the old
	// loop has processed cmdKill.
	m.controlLoopsMu.Lock()
	if loop, exists := m.controlLoops[nodeID]; exists {
		loop.cancel()
		delete(m.controlLoops, nodeID)
	}
	m.controlLoopsMu.Unlock()

	m.nodeStates.RemoveNodeState(nodeID)
}

func (m *manager) GetListenPort() int {
	return m.actualPort
}

func (m *manager) IsManaged(nodeID cluster.NodeID) bool {
	return m.nodeStates.GetNodeState(nodeID) != nil
}

// RecordDropReason exposes the unexported telemetry counter for callers that
// drop messages outside the per-class queue path (RX delivery failures, TX
// pre-send encode failures, etc.). The label "class" is set to "unknown"
// because those drop sites don't carry class context.
func (m *manager) RecordDropReason(reason string) {
	m.nodeStates.tel.recordDropReason(reason)
}

// RegisterClassReceiver claims a sub-protocol Class so inbound frames with
// that class bypass the default onMessage callback and go straight to recv.
// Idempotent: registering nil clears the receiver. Returns false if a
// non-nil receiver is already registered for that class.
//
// Registrations are rare (boot-time) but lookups happen on every inbound
// frame, so we publish a fresh snapshot via atomic.Pointer and lookups
// skip the mutex entirely.
func (m *manager) RegisterClassReceiver(class Class, recv func(cluster.NodeID, []byte)) bool {
	if int(class) >= numClasses {
		return false
	}
	m.registerMu.Lock()
	defer m.registerMu.Unlock()
	var next [numClasses]func(cluster.NodeID, []byte)
	if cur := m.classReceivers.Load(); cur != nil {
		next = *cur
	}
	if recv != nil && next[class] != nil {
		return false
	}
	next[class] = recv
	m.classReceivers.Store(&next)
	return true
}

func (m *manager) lookupClassReceiver(class Class) func(cluster.NodeID, []byte) {
	if int(class) >= numClasses {
		return nil
	}
	snap := m.classReceivers.Load()
	if snap == nil {
		return nil
	}
	return snap[class]
}

// RegisterClassOverflowHandler claims the overflow-reset hook for a Class.
// Registering nil clears the handler. Returns false if a non-nil handler is
// already registered for that class. Snapshot semantics match
// RegisterClassReceiver.
func (m *manager) RegisterClassOverflowHandler(class Class, handler func(cluster.NodeID)) bool {
	if int(class) >= numClasses {
		return false
	}
	m.registerMu.Lock()
	defer m.registerMu.Unlock()
	var next [numClasses]func(cluster.NodeID)
	if cur := m.classOverflow.Load(); cur != nil {
		next = *cur
	}
	if handler != nil && next[class] != nil {
		return false
	}
	next[class] = handler
	m.classOverflow.Store(&next)
	return true
}

func (m *manager) lookupClassOverflow(class Class) func(cluster.NodeID) {
	if int(class) >= numClasses {
		return nil
	}
	snap := m.classOverflow.Load()
	if snap == nil {
		return nil
	}
	return snap[class]
}

// EvictOrphanNodes walks the controlLoops + nodeStates and removes any
// node not in `known`. Returns the count of removals. Caller passes a
// snapshot of the membership view as the authoritative truth.
func (m *manager) EvictOrphanNodes(known map[cluster.NodeID]struct{}) int {
	if known == nil {
		return 0
	}
	// Find orphans under the controlLoops lock so we get a consistent
	// snapshot of which nodes the manager believes are alive.
	var orphans []cluster.NodeID
	m.controlLoopsMu.Lock()
	for nodeID := range m.controlLoops {
		if _, ok := known[nodeID]; !ok && nodeID != m.config.LocalNodeID {
			orphans = append(orphans, nodeID)
		}
	}
	m.controlLoopsMu.Unlock()

	// Also catch nodes that have nodeState but no controlLoop (auto-managed
	// from inbound connection that never produced traffic).
	m.nodeStates.nodeStates.Range(func(key, _ any) bool {
		nodeID := key.(cluster.NodeID)
		if nodeID == m.config.LocalNodeID {
			return true
		}
		if _, ok := known[nodeID]; ok {
			return true
		}
		// Avoid double-add if already in the orphans slice from controlLoops.
		for _, existing := range orphans {
			if existing == nodeID {
				return true
			}
		}
		orphans = append(orphans, nodeID)
		return true
	})

	for _, nodeID := range orphans {
		m.RemoveManagedNode(nodeID)
		m.nodeStates.tel.recordEviction("orphan")
	}
	return len(orphans)
}

func (m *manager) sendCommand(nodeID cluster.NodeID, cmd nodeCommand) {
	m.controlLoopsMu.Lock()
	loop, exists := m.controlLoops[nodeID]
	if !exists {
		// Before creating a loop, verify the underlying state exists.
		if m.nodeStates.GetNodeState(nodeID) == nil {
			m.controlLoopsMu.Unlock()
			m.logger.Error("Attempted to create control loop for unmanaged node", zap.String("node", nodeID))
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
			logger:     m.logger.With(zap.String("node", nodeID)),
			retryDelay: m.config.InitialRetryDelay,
		}
		m.controlLoops[nodeID] = loop

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer m.cleanupControlLoop(nodeID, loop)
			loop.run()
		}()
	}
	m.controlLoopsMu.Unlock()

	select {
	case loop.commands <- cmd:
	case <-loop.ctx.Done():
	}
}

func (m *manager) cleanupControlLoop(nodeID cluster.NodeID, self *nodeControlLoop) {
	m.controlLoopsMu.Lock()
	// Only delete the map entry if it still points to THIS loop.
	// A replacement loop may have been created (e.g. by AddManagedNode
	// tearing down the old loop and sendCommand creating a new one).
	if current, exists := m.controlLoops[nodeID]; exists && current == self {
		delete(m.controlLoops, nodeID)
	}
	m.controlLoopsMu.Unlock()
	m.logger.Debug("Control loop cleaned up", zap.String("node", nodeID))
}

func (loop *nodeControlLoop) run() {
	loop.logger.Debug("Control loop started")
	defer loop.logger.Debug("Control loop stopped")

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
	loop.logger.Debug("handleConnect called",
		zap.String("addr", data.Addr),
		zap.Int("port", data.Port),
		zap.String("state", loop.state.String()))
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
	loop.logger.Debug("handleConnected called",
		zap.String("state", loop.state.String()),
		zap.Bool("has_connection", data.Connection != nil))
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

	// Wire the connection's writeLoop to drain this node's per-class queues
	// directly. It self-drains anything buffered while the node was
	// disconnected, so no explicit drain call is needed here.
	loop.bindConnectionDrain()

	// Capture the connection now and hand it to the monitor goroutine.
	// Reading loop.connection inside the goroutine races with cleanup
	// paths that set loop.connection = nil on disconnect/kill.
	conn := loop.connection
	loop.manager.wg.Add(1)
	go func() {
		defer loop.manager.wg.Done()
		loop.monitorConnection(conn)
	}()
}

// bindConnectionDrain wires loop.connection's writeLoop to this node's
// per-class outbound queues. Must be called before the connection's Run.
func (loop *nodeControlLoop) bindConnectionDrain() {
	nodeID := loop.nodeID
	nsm := loop.manager.nodeStates
	notify := nsm.GetMessageNotifier(nodeID)
	if notify == nil {
		loop.logger.Error("no message notifier for managed node", zap.String("node", nodeID))
		notify = make(chan struct{})
	}
	loop.connection.bindDrain(
		notify,
		func(n int) []Outbound { return nsm.DrainMessages(nodeID, n) },
		func(b []Outbound) { nsm.RequeueMessages(nodeID, b) },
		loop.manager.config.DrainBatchSize,
	)
}

func (loop *nodeControlLoop) handleDisconnected(data disconnectedData) {
	loop.logger.Debug("handleDisconnected called",
		zap.String("state", loop.state.String()),
		zap.Bool("should_retry", data.ShouldRetry),
		zap.Error(data.Error),
		zap.Int("retry_count", loop.retryCount))
	if loop.state == StateDead {
		return
	}
	// Un-drained messages remain in the per-class queues — a subsequent
	// connection's writeLoop delivers them. Only the writeLoop's own
	// in-flight batch needs requeue, and it handles that itself on a
	// write failure before returning.
	if loop.connection != nil {
		loop.connection.Close()
		loop.connection = nil
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
		loop.connection.Close()
		loop.connection = nil
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
	addr := net.JoinHostPort(loop.addr, fmt.Sprintf("%d", loop.port))
	loop.logger.Debug("Attempting outbound connection", zap.String("target_addr", addr))
	var conn net.Conn
	var err error
	if loop.manager.tlsConfig != nil {
		dialer := &tls.Dialer{
			NetDialer: &net.Dialer{Timeout: loop.manager.config.HandshakeTimeout},
			Config:    loop.manager.tlsConfig,
		}
		conn, err = dialer.DialContext(loop.ctx, "tcp", addr)
	} else {
		dialer := &net.Dialer{Timeout: loop.manager.config.HandshakeTimeout}
		conn, err = dialer.DialContext(loop.ctx, "tcp", addr)
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

func (loop *nodeControlLoop) monitorConnection(conn *NodeConnection) {
	if conn == nil {
		return
	}
	err := conn.Run(func(class Class, msg []byte) {
		if recv := loop.manager.lookupClassReceiver(class); recv != nil {
			recv(loop.nodeID, msg)
			return
		}
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
	lc := &net.ListenConfig{}
	if m.tlsConfig != nil {
		ln, err := lc.Listen(m.ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		return tls.NewListener(ln, m.tlsConfig), nil
	}
	return lc.Listen(m.ctx, "tcp", addr)
}

func (m *manager) acceptLoop() {
	m.logger.Debug("Accept loop started", zap.Int("listen_port", m.actualPort))
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			if m.ctx.Err() != nil {
				m.logger.Debug("Accept loop stopping (context cancelled)")
				return
			}
			m.logger.Error("Failed to accept connection", zap.Error(err))
			continue
		}
		m.logger.Debug("Accepted inbound TCP connection",
			zap.String("remote_addr", conn.RemoteAddr().String()),
			zap.String("local_addr", conn.LocalAddr().String()))
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.handleInboundConnection(conn)
		}()
	}
}

func (m *manager) handleInboundConnection(conn net.Conn) {
	m.logger.Debug("Handling inbound connection",
		zap.String("remote_addr", conn.RemoteAddr().String()),
		zap.String("local_node", m.config.LocalNodeID))

	nodeConn, err := PerformServerHandshake(conn, m.config.NodeConnectionConfig(), m.logger, m.config.LocalNodeID)
	if err != nil {
		m.logger.Warn("Inbound handshake failed", zap.Error(err), zap.String("remote_addr", conn.RemoteAddr().String()))
		return
	}
	remoteNodeID := nodeConn.RemoteNodeID()
	m.logger.Debug("Inbound handshake succeeded", zap.String("remote_node", remoteNodeID))

	// Auto-manage the node if not already managed. This handles the race where
	// the remote node's outbound connection arrives before the local NodeJoined
	// event is processed (which would call AddManagedNode).
	if m.nodeStates.GetNodeState(remoteNodeID) == nil {
		m.logger.Info("Auto-managing node from inbound connection", zap.String("node", remoteNodeID))
		m.nodeStates.CreateNodeState(remoteNodeID)
	}

	_, currentState := m.nodeStates.GetNodeConnection(remoteNodeID)
	if currentState == StateConnected {
		m.logger.Debug("Already connected, dropping new inbound connection", zap.String("node", remoteNodeID))
		nodeConn.Close()
		return
	}

	if m.shouldDropInbound(remoteNodeID) {
		m.logger.Debug("Dropping inbound connection due to tie-breaking", zap.String("node", remoteNodeID))
		nodeConn.Close()
		return
	}

	m.logger.Debug("Accepting inbound connection, sending cmdConnected", zap.String("remote_node", remoteNodeID))
	m.sendCommand(remoteNodeID, nodeCommand{
		Type: cmdConnected,
		Data: connectedData{Connection: nodeConn},
	})
}

func (m *manager) shouldInitiateConnection(remoteNodeID cluster.NodeID) bool {
	return strings.Compare(m.config.LocalNodeID, remoteNodeID) < 0
}

func (m *manager) shouldDropInbound(remoteNodeID cluster.NodeID) bool {
	return m.shouldInitiateConnection(remoteNodeID)
}

func loadTLSConfig(cfg ManagerTLSConfig) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, NewLoadKeyPairError(err)
	}

	caCertPool := x509.NewCertPool()
	caCert, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, NewReadCACertError(err)
	}
	if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
		return nil, ErrFailedToAppendCACerts
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}, nil
}
