// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
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
	AuthorizePeer     func(cluster.NodeID, net.Addr) bool
	ResolvePeerKey    func(cluster.NodeID) (ed25519.PublicKey, bool)
	BindAddr          string
	LocalNodeID       cluster.NodeID
	AuthenticationKey []byte
	SigningKey        ed25519.PrivateKey
	TLS               ManagerTLSConfig
	MaxRetryAttempts  int
	BindPort          int
	CommandQueueSize  int
	HandshakeTimeout  time.Duration
	OutboundQueueSize int
	// Reliable budgets cover queued and writer-owned frames. Zero uses defaults.
	OutboundPeerBytes    uint64
	OutboundTotalBytes   uint64
	OutboundTotalEntries uint64
	// Control reserves are carved out of the reliable totals. Zero disables that
	// reserve; explicit values must leave capacity for ordinary traffic.
	OutboundControlPeerEntries  uint64
	OutboundControlPeerBytes    uint64
	OutboundControlTotalEntries uint64
	OutboundControlTotalBytes   uint64
	// GossipQueueCap bounds SWIM/memberlist-style gossip. Gossip is the only
	// intentionally lossy internode class; reliable actor and raft classes
	// queue while the peer remains managed and are discarded only on node
	// removal.
	GossipQueueCap int

	InitialRetryDelay     time.Duration
	MaxRetryDelay         time.Duration
	DrainBatchSize        int
	DrainBatchBytes       uint64
	MaxMessageSize        uint32
	AutoPort              bool
	RequireAuthentication bool
}

func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		HandshakeTimeout:            5 * time.Second,
		OutboundQueueSize:           256,
		OutboundPeerBytes:           512 * 1024 * 1024,
		OutboundTotalBytes:          2 * 1024 * 1024 * 1024,
		OutboundTotalEntries:        4096,
		OutboundControlPeerEntries:  16,
		OutboundControlPeerBytes:    1 << 20,
		OutboundControlTotalEntries: 256,
		OutboundControlTotalBytes:   16 << 20,
		MaxMessageSize:              512 * 1024 * 1024,
		TLS:                         ManagerTLSConfig{Enabled: false},
		InitialRetryDelay:           10 * time.Millisecond,
		MaxRetryDelay:               5 * time.Second,
		AutoPort:                    true,
		BindPort:                    DefaultPortRangeStart,
		DrainBatchSize:              32,
		DrainBatchBytes:             1 << 20,
		CommandQueueSize:            256,
		MaxRetryAttempts:            10,
		GossipQueueCap:              1024,
		RequireAuthentication:       true,
	}
}

func (mc ManagerConfig) NodeConnectionConfig() NodeConnectionConfig {
	return NodeConnectionConfig{
		AuthorizePeer:         mc.AuthorizePeer,
		ResolvePeerKey:        mc.ResolvePeerKey,
		HandshakeTimeout:      mc.HandshakeTimeout,
		AuthenticationKey:     append([]byte(nil), mc.AuthenticationKey...),
		SigningKey:            append(ed25519.PrivateKey(nil), mc.SigningKey...),
		MaxMessageSize:        mc.MaxMessageSize,
		RequireAuthentication: mc.RequireAuthentication,
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
	commandMu      sync.Mutex
	commandClosed  bool
	commandSenders sync.WaitGroup
	ctx            context.Context
	manager        *manager
	commands       chan nodeCommand
	connection     *NodeConnection
	logger         *zap.Logger
	cancel         context.CancelFunc
	nodeID         cluster.NodeID
	nodeState      *NodeState
	addr           string
	state          ConnectionState
	retryDelay     time.Duration
	retryCount     int
	port           int
	isOutbound     bool
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
	// uses this to claim ClassRaftRPC; the default class set continues
	// to flow through Start's onMessage.
	RegisterClassReceiver(class Class, recv func(nodeID cluster.NodeID, data []byte)) bool
}

type manager struct {
	stopping       atomic.Bool
	stopOnce       sync.Once
	managedChanged chan struct{}
	ctx            context.Context
	listener       net.Listener
	cancel         context.CancelFunc
	logger         *zap.Logger
	onMessage      func(cluster.NodeID, []byte)
	tlsConfig      *tls.Config
	nodeStates     *NodeStateManager
	controlLoops   map[cluster.NodeID]*nodeControlLoop
	// classReceivers is accessed on every inbound frame (lookupClassReceiver
	// runs in the read hot path). Registrations happen only at boot, so we
	// keep the array behind an atomic.Pointer snapshot.
	classReceivers atomic.Pointer[[numClasses]func(cluster.NodeID, []byte)]
	config         ManagerConfig
	wg             sync.WaitGroup
	actualPort     int
	controlLoopsMu sync.Mutex
	registerMu     sync.Mutex
	managedMu      sync.Mutex
}

func NewConnectionManager(config ManagerConfig, coll metrics.Collector) ConnectionManager {
	config.AuthenticationKey = append([]byte(nil), config.AuthenticationKey...)
	config.SigningKey = append(ed25519.PrivateKey(nil), config.SigningKey...)
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
	m.controlLoopsMu.Lock()
	defer m.controlLoopsMu.Unlock()
	if m.stopping.Load() {
		return ErrNodeNotManaged
	}
	if m.nodeStates.budgetErr != nil {
		return m.nodeStates.budgetErr
	}
	if m.config.RequireAuthentication {
		if len(m.config.AuthenticationKey) == 0 {
			return fmt.Errorf("internode authentication key is required")
		}
		if len(m.config.SigningKey) != ed25519.PrivateKeySize {
			return fmt.Errorf("internode signing key is required")
		}
		if m.config.ResolvePeerKey == nil {
			return fmt.Errorf("internode peer key resolver is required")
		}
		if m.config.AuthorizePeer == nil {
			return fmt.Errorf("internode peer authorizer is required")
		}
	}
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
	m.stopOnce.Do(func() {
		m.controlLoopsMu.Lock()
		m.stopping.Store(true)
		m.controlLoopsMu.Unlock()
		m.logger.Info("Stopping connection manager...")
		m.nodeStates.reliable.close()
		if m.cancel != nil {
			m.cancel()
		}
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
		m.nodeStates.nodeStates.Range(func(key, _ any) bool {
			m.nodeStates.RemoveNodeState(key.(cluster.NodeID))
			return true
		})
		m.logger.Info("Connection manager stopped")
	})
	return nil
}

// ContextConnectionManager is optional transactional queue admission. Error
// means data was not retained; success does not guarantee remote delivery.
type ContextConnectionManager interface {
	SendToNodeContext(context.Context, cluster.NodeID, []byte, Class) error
}

func (m *manager) SendToNodeContext(ctx context.Context, nodeID cluster.NodeID, data []byte, class Class) error {
	if m.stopping.Load() {
		return ErrNodeNotManaged
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.ctx != nil {
		if err := m.ctx.Err(); err != nil {
			return err
		}
		admissionCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(m.ctx, cancel)
		defer stop()
		defer cancel()
		ctx = admissionCtx
	}
	// An unmanaged destination is a refusal in both send paths; cancellation
	// additionally bounds waiting for queue admission here.
	return m.nodeStates.QueueMessageClassContext(ctx, nodeID, data, class)
}

func (m *manager) SendToNode(nodeID cluster.NodeID, data []byte, class Class) error {
	if m.stopping.Load() {
		return ErrNodeNotManaged
	}
	err := m.nodeStates.QueueMessageClass(nodeID, data, class)
	if err != nil {
		if errors.Is(err, ErrNodeNotManaged) {
			// Preserve the quiet metric on partition hot paths, but report refusal:
			// counting a drop is not successful queue admission.
			m.nodeStates.tel.recordDrop(class, "node_not_managed")
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

	m.nodeStates.UpdateNodeAddress(nodeID, addr, port)
	_, currentState := m.nodeStates.GetNodeConnection(nodeID)
	if currentState == StateConnected {
		return
	}

	if !m.shouldInitiateConnection(nodeID) {
		return
	}

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
	defer m.notifyManagedChange()
	// Serialize membership and inbound admission with state detachment.
	m.controlLoopsMu.Lock()
	defer m.controlLoopsMu.Unlock()
	if m.stopping.Load() {
		return
	}
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
	if oldLoop, exists := m.controlLoops[nodeID]; exists {
		m.logger.Info("Tearing down stale control loop for rejoining node", zap.String("node", nodeID))
		oldLoop.cancel()
		delete(m.controlLoops, nodeID)
	}

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
	state := m.nodeStates.detachNodeState(nodeID)
	m.controlLoopsMu.Unlock()

	// Close connections and drain old queues without holding the lifecycle lock.
	m.nodeStates.closeDetachedNodeState(nodeID, state)
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

// A connection carried by an unaccepted command remains owned by its sender.
func discardNodeCommand(cmd nodeCommand) {
	if data, ok := cmd.Data.(connectedData); ok && data.Connection != nil {
		data.Connection.Close()
	}
}

func (m *manager) sendCommand(nodeID cluster.NodeID, cmd nodeCommand) {
	m.controlLoopsMu.Lock()
	if m.stopping.Load() {
		m.controlLoopsMu.Unlock()
		discardNodeCommand(cmd)
		return
	}

	loop, exists := m.controlLoops[nodeID]
	if !exists {
		// Before creating a loop, verify the underlying state exists.
		state := m.nodeStates.GetNodeState(nodeID)
		if state == nil {
			m.controlLoopsMu.Unlock()
			m.logger.Error("Attempted to create control loop for unmanaged node", zap.String("node", nodeID))
			discardNodeCommand(cmd)
			return
		}

		ctx, cancel := context.WithCancel(m.ctx)
		loop = &nodeControlLoop{
			nodeID:     nodeID,
			nodeState:  state,
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

	loop.enqueueCommand(cmd)
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
	if !loop.manager.nodeStates.setNodeStateForState(loop.nodeID, loop.nodeState, loop.state) {
		return
	}

	addr, port := loop.addr, loop.port
	loop.manager.wg.Add(1)
	go func() {
		defer loop.manager.wg.Done()
		loop.attemptConnection(addr, port)
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
	if !loop.manager.nodeStates.setNodeConnectionForState(loop.nodeID, loop.nodeState, loop.connection, loop.state) {
		if loop.connection != nil {
			loop.connection.Close()
		}
		loop.connection = nil
		loop.state = StateNone
		return
	}
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
	state := loop.nodeState
	state.queueMu.Lock()
	generation := state.generation
	state.queueMu.Unlock()
	notify := state.messageNotify
	if notify == nil {
		loop.logger.Error("no message notifier for managed node", zap.String("node", nodeID))
		notify = make(chan struct{})
	}
	loop.connection.bindDrain(
		notify,
		func(n int) []Outbound { return nsm.drainMessagesForGeneration(nodeID, state, generation, n) },
		func(b []Outbound) { nsm.requeueMessagesForState(nodeID, state, b) },
		loop.manager.config.DrainBatchSize,
		releaseOutbound,
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
	loop.manager.nodeStates.setNodeConnectionForState(loop.nodeID, loop.nodeState, nil, StateNone)

	if data.ShouldRetry && loop.isOutbound && loop.retryCount < loop.manager.config.MaxRetryAttempts {
		loop.state = StateRetrying
		loop.retryCount++
		if loop.retryDelay < loop.manager.config.MaxRetryDelay {
			loop.retryDelay *= 2
		}
		fallbackAddr, fallbackPort := loop.addr, loop.port
		time.AfterFunc(loop.retryDelay, func() {
			addr, port, hasAddr := loop.manager.nodeStates.getNodeAddressForState(loop.nodeID, loop.nodeState)
			if !hasAddr {
				addr, port = fallbackAddr, fallbackPort
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
	// Seal before cancel/join: producers may choose a writable buffer even when
	// cancellation is ready. Once admitted producers return, drain every command
	// still owned by this queue, including completed handshake connections.
	loop.commandMu.Lock()
	loop.commandClosed = true
	if loop.cancel != nil {
		loop.cancel()
	}
	loop.commandMu.Unlock()
	loop.commandSenders.Wait()
	for {
		select {
		case cmd := <-loop.commands:
			discardNodeCommand(cmd)
		default:
			goto drained
		}
	}
drained:

	if loop.connection != nil {
		loop.connection.Close()
		loop.connection = nil
	}
	loop.manager.nodeStates.setNodeConnectionForState(loop.nodeID, loop.nodeState, nil, StateNone)
}

func (loop *nodeControlLoop) sendCommandToSelf(cmd nodeCommand) {
	loop.enqueueCommand(cmd)
}

func (loop *nodeControlLoop) enqueueCommand(cmd nodeCommand) {
	loop.commandMu.Lock()
	if loop.commandClosed || loop.ctx.Err() != nil {
		loop.commandMu.Unlock()
		discardNodeCommand(cmd)
		return
	}
	loop.commandSenders.Add(1)
	loop.commandMu.Unlock()
	defer loop.commandSenders.Done()
	select {
	case loop.commands <- cmd:
	case <-loop.ctx.Done():
		discardNodeCommand(cmd)
	}
}

func (loop *nodeControlLoop) attemptConnection(addr string, port int) {
	if loop.ctx.Err() != nil {
		return
	}
	targetAddr := net.JoinHostPort(addr, fmt.Sprintf("%d", port))
	loop.logger.Debug("Attempting outbound connection", zap.String("target_addr", targetAddr))
	var conn net.Conn
	var err error
	if loop.manager.tlsConfig != nil {
		dialer := &tls.Dialer{
			NetDialer: &net.Dialer{Timeout: loop.manager.config.HandshakeTimeout},
			Config:    loop.manager.tlsConfig,
		}
		conn, err = dialer.DialContext(loop.ctx, "tcp", targetAddr)
	} else {
		dialer := &net.Dialer{Timeout: loop.manager.config.HandshakeTimeout}
		conn, err = dialer.DialContext(loop.ctx, "tcp", targetAddr)
	}
	if err != nil {
		loop.sendDisconnected(err, true)
		return
	}
	stopCancellation := bindHandshakeCancellation(loop.ctx, conn)
	nodeConn, err := PerformClientHandshake(conn, loop.manager.config.NodeConnectionConfig(), loop.logger, loop.manager.config.LocalNodeID, loop.nodeID)
	stopCancellation()
	if err != nil {
		loop.sendDisconnected(err, true)
		return
	}
	if loop.ctx.Err() != nil {
		nodeConn.Close()
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
	addr := net.JoinHostPort(m.config.BindAddr, strconv.Itoa(m.config.BindPort))
	listener, err := m.listen(addr)
	if err != nil {
		return nil, 0, err
	}
	return listener, listener.Addr().(*net.TCPAddr).Port, nil
}

func (m *manager) tryPortRange() (net.Listener, int, error) {
	startPort := m.config.BindPort
	if startPort == 0 {
		listener, err := m.listen(net.JoinHostPort(m.config.BindAddr, "0"))
		if err != nil {
			return nil, 0, err
		}
		return listener, listener.Addr().(*net.TCPAddr).Port, nil
	}
	for port := startPort; port <= DefaultPortRangeEnd; port++ {
		addr := net.JoinHostPort(m.config.BindAddr, strconv.Itoa(port))
		if listener, err := m.listen(addr); err == nil {
			return listener, port, nil
		}
	}
	addr := net.JoinHostPort(m.config.BindAddr, "0")
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

	// The handshake owns the accepted socket until it hands off a session.
	// Cancellation aborts the underlying transport, including a stalled TLS
	// negotiation. Join an executing cancellation callback before handoff.
	stopCancellation := bindHandshakeCancellation(m.ctx, conn)
	nodeConn, err := PerformServerHandshake(conn, m.config.NodeConnectionConfig(), m.logger, m.config.LocalNodeID)
	stopCancellation()
	if err != nil {
		m.logger.Warn("Inbound handshake failed", zap.Error(err), zap.String("remote_addr", conn.RemoteAddr().String()))
		return
	}
	if m.stopping.Load() || (m.ctx != nil && m.ctx.Err() != nil) {
		nodeConn.Close()
		return
	}
	remoteNodeID := nodeConn.RemoteNodeID()
	m.logger.Debug("Inbound handshake succeeded", zap.String("remote_node", remoteNodeID))

	if m.nodeStates.GetNodeState(remoteNodeID) == nil {
		if m.config.AuthorizePeer == nil || !m.config.AuthorizePeer(remoteNodeID, conn.RemoteAddr()) {
			m.logger.Warn("Rejecting unmanaged inbound node", zap.String("node", remoteNodeID))
			nodeConn.Close()
			return
		}
		m.logger.Info("Auto-managing authorized inbound node", zap.String("node", remoteNodeID))
		m.AddManagedNode(remoteNodeID)
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
