// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"errors"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

var (
	// ErrNodeNotManaged is returned when an operation is attempted on a node
	// that has not been explicitly registered as a cluster member.
	ErrNodeNotManaged = errors.New("node is not a managed member of the cluster")

	// ErrQueueFull is returned by QueueMessageClass when the gossip queue is
	// at capacity. Gossip is intentionally lossy; reliable actor and raft
	// classes do not overflow while the node remains managed.
	ErrQueueFull = errors.New("internode: send queue is full")

	// ErrUnknownClass is returned by QueueMessageClass when the class value
	// is out of range. This is a programmer error, not a runtime condition.
	ErrUnknownClass = errors.New("internode: unknown queue class (programmer error)")
)

type NodeState struct {
	createdAt     time.Time // for observability: when this state was first created
	queues        [numClasses]*classQueue
	messageNotify chan struct{}
	connection    *NodeConnection
	address       nodeAddress
	lastDepth     [numClasses]int // last queue depth emitted to telemetry; guarded by queueMu
	state         ConnectionState
	stateMu       sync.RWMutex
	queueMu       sync.Mutex
}

// classQueue is a FIFO of pending messages for one Class.
// All access is guarded by NodeState.queueMu (held for cross-class
// operations). A zero capacity is unbounded.
type classQueue struct {
	buf       [][]byte
	head      int // index of the oldest element (next to drain)
	size      int // number of valid entries
	unbounded bool
}

func newClassQueue(cap int) *classQueue {
	if cap <= 0 {
		return &classQueue{unbounded: true}
	}
	return &classQueue{buf: make([][]byte, cap)}
}

// pushNewest appends if there is room. Returns false if full (no insert).
func (q *classQueue) pushNewest(data []byte) (accepted bool) {
	if q.unbounded {
		q.buf = append(q.buf, data)
		q.size++
		return true
	}
	if q.size == len(q.buf) {
		return false
	}
	tail := (q.head + q.size) % len(q.buf)
	q.buf[tail] = data
	q.size++
	return true
}

// pushFront inserts at the front for requeue (callers must respect cap).
// Returns false if full.
func (q *classQueue) pushFront(data []byte) (accepted bool) {
	if q.unbounded {
		if q.head > 0 {
			q.head--
			q.buf[q.head] = data
		} else {
			q.buf = append(q.buf, nil)
			copy(q.buf[1:], q.buf)
			q.buf[0] = data
		}
		q.size++
		return true
	}
	if q.size == len(q.buf) {
		return false
	}
	q.head = (q.head - 1 + len(q.buf)) % len(q.buf)
	q.buf[q.head] = data
	q.size++
	return true
}

// pop removes and returns the oldest entry; ok=false when empty.
func (q *classQueue) pop() (data []byte, ok bool) {
	if q.size == 0 {
		return nil, false
	}
	if q.unbounded {
		data = q.buf[q.head]
		q.buf[q.head] = nil
		q.head++
		q.size--
		if q.size == 0 {
			q.head = 0
			q.buf = q.buf[:0]
		} else if q.head > 1024 && q.head*2 >= len(q.buf) {
			copy(q.buf, q.buf[q.head:])
			for i := q.size; i < len(q.buf); i++ {
				q.buf[i] = nil
			}
			q.buf = q.buf[:q.size]
			q.head = 0
		}
		return data, true
	}
	data = q.buf[q.head]
	q.buf[q.head] = nil // release reference
	q.head = (q.head + 1) % len(q.buf)
	q.size--
	return data, true
}

// reset drops all entries. Allocations remain.
func (q *classQueue) reset() {
	for i := range q.buf {
		q.buf[i] = nil
	}
	if q.unbounded {
		q.buf = q.buf[:0]
	}
	q.head = 0
	q.size = 0
}

func (q *classQueue) len() int { return q.size }

type nodeAddress struct {
	addr string
	port int
}

type NodeStateManager struct {
	nodeStates sync.Map // cluster.NodeID -> *NodeState
	logger     *zap.Logger
	tel        *telemetry
	config     ManagerConfig
}

func NewNodeStateManager(config ManagerConfig, tel *telemetry, logger *zap.Logger) *NodeStateManager {
	return &NodeStateManager{
		logger: logger.Named("state"),
		tel:    tel,
		config: config,
	}
}

// CreateNodeState initializes the in-memory state for a new node.
// This should only be called by the manager when a node joins the cluster.
// If state already exists (e.g. stale entry from a previous incarnation),
// the existing struct is reused: connection is closed and replaced, queue and
// state are reset, but the messageNotify channel is kept so any existing
// control loop continues to receive notifications without holding a stale
// channel reference.
//
// Auto-managed nodes (created from inbound connections before the formal
// NodeJoined event) are cleaned up when their connection closes; no separate
// reaper goroutine is needed.
func (nsm *NodeStateManager) CreateNodeState(nodeID cluster.NodeID) {
	if existing, ok := nsm.nodeStates.Load(nodeID); ok {
		oldState := existing.(*NodeState)

		// Reset connection
		oldState.stateMu.Lock()
		if oldState.connection != nil {
			oldState.connection.Close()
			oldState.connection = nil
		}
		oldState.state = StateNone
		oldState.address = nodeAddress{}
		oldState.stateMu.Unlock()

		// Reset all queues
		oldState.queueMu.Lock()
		for i := range oldState.queues {
			oldState.queues[i].reset()
		}
		oldState.lastDepth = [numClasses]int{}
		oldState.queueMu.Unlock()

		// Do NOT replace messageNotify — existing control loops hold a reference.
		nsm.logger.Debug("Reset existing state for rejoining node", zap.String("node_id", nodeID))
		return
	}

	caps := [numClasses]int{
		ClassRaftControl: 0,
		ClassGossip:      nsm.config.GossipQueueCap,
		ClassPGBroadcast: 0,
		ClassRaftRPC:     0,
	}
	queues := [numClasses]*classQueue{}
	for i := range queues {
		queues[i] = newClassQueue(caps[i])
	}
	newState := &NodeState{
		queues:        queues,
		messageNotify: make(chan struct{}, 1),
		state:         StateNone,
		createdAt:     time.Now(),
	}
	nsm.nodeStates.Store(nodeID, newState)
}

func (nsm *NodeStateManager) GetNodeState(nodeID cluster.NodeID) *NodeState {
	state, ok := nsm.nodeStates.Load(nodeID)
	if !ok {
		return nil
	}
	return state.(*NodeState)
}

// QueueMessageClass enqueues data for nodeID under the given class.
// Delivery policy is class-specific:
//   - ClassRaftControl, ClassPGBroadcast, and ClassRaftRPC are reliable
//     while the peer remains managed.
//   - ClassGossip drops the new entry and returns ErrQueueFull when full.
//
// In all drop cases, internode_dropped_total{class,reason="queue_full"}
// is incremented.
//
// Returns ErrNodeNotManaged if no state exists for nodeID.
// Returns ErrQueueFull for gossip when full.
func (nsm *NodeStateManager) QueueMessageClass(nodeID cluster.NodeID, data []byte, class Class) error {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return ErrNodeNotManaged
	}
	if data == nil {
		return nil
	}
	if int(class) >= numClasses {
		return ErrUnknownClass
	}

	state.queueMu.Lock()
	q := state.queues[class]
	var rejected bool
	switch class {
	case ClassRaftControl, ClassPGBroadcast, ClassRaftRPC:
		q.pushNewest(data)
	case ClassGossip:
		if !q.pushNewest(data) {
			rejected = true
		}
	}
	depth := q.len()
	depthChanged := depth != state.lastDepth[class]
	state.lastDepth[class] = depth
	state.queueMu.Unlock()

	// internode_queue_depth is a gauge — emit only on change so the idle
	// hot path does not write a metric event per queue op.
	if depthChanged {
		nsm.tel.recordQueueDepth(class, nodeID, depth)
	}

	if rejected {
		nsm.tel.recordDrop(class, "queue_full")
		return ErrQueueFull
	}

	select {
	case state.messageNotify <- struct{}{}:
	default:
	}
	return nil
}

func (nsm *NodeStateManager) SetNodeConnection(nodeID cluster.NodeID, conn *NodeConnection, newState ConnectionState) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		nsm.logger.Warn("Attempted to set connection for an unmanaged node", zap.String("node_id", nodeID))
		return
	}

	state.stateMu.Lock()
	state.connection = conn
	state.state = newState
	state.stateMu.Unlock()
}

func (nsm *NodeStateManager) GetNodeConnection(nodeID cluster.NodeID) (*NodeConnection, ConnectionState) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return nil, StateNone
	}

	state.stateMu.RLock()
	conn := state.connection
	currentState := state.state
	state.stateMu.RUnlock()

	return conn, currentState
}

func (nsm *NodeStateManager) SetNodeState(nodeID cluster.NodeID, newState ConnectionState) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		nsm.logger.Warn("Attempted to set state for an unmanaged node", zap.String("node_id", nodeID))
		return
	}

	state.stateMu.Lock()
	state.state = newState
	state.stateMu.Unlock()
}

func (nsm *NodeStateManager) UpdateNodeAddress(nodeID cluster.NodeID, addr string, port int) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		nsm.logger.Warn("Attempted to update address for an unmanaged node", zap.String("node_id", nodeID))
		return
	}

	state.stateMu.Lock()
	state.address = nodeAddress{addr: addr, port: port}
	state.stateMu.Unlock()
}

func (nsm *NodeStateManager) GetNodeAddress(nodeID cluster.NodeID) (string, int, bool) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return "", 0, false
	}

	state.stateMu.RLock()
	addr := state.address
	state.stateMu.RUnlock()

	return addr.addr, addr.port, addr.addr != "" && addr.port != 0
}

// drainClasses defines the QoS draining order. ClassRaftControl drains
// first (smallest latency budget); ClassRaftRPC second so raft RPC
// frames stay responsive; gossip and PG broadcast last. The order
// matters under per-batch caps: a saturated control plane should not
// starve raft RPC traffic forever.
var drainClasses = [numClasses]Class{
	ClassRaftControl,
	ClassRaftRPC,
	ClassGossip,
	ClassPGBroadcast,
}

func (nsm *NodeStateManager) DrainMessages(nodeID cluster.NodeID, maxCount int) []Outbound {
	state := nsm.GetNodeState(nodeID)
	if state == nil || maxCount <= 0 {
		return nil
	}

	state.queueMu.Lock()
	out := make([]Outbound, 0, maxCount)
	for _, class := range drainClasses {
		q := state.queues[class]
		for q.len() > 0 && len(out) < maxCount {
			d, _ := q.pop()
			if d != nil {
				out = append(out, Outbound{Data: d, Class: class})
			}
		}
		if len(out) >= maxCount {
			break
		}
	}
	// Snapshot post-drain depths. internode_queue_depth is a gauge — emit
	// only the classes whose depth changed so an idle drain does not write
	// numClasses no-op metric events.
	var depths [numClasses]int
	var depthChanged [numClasses]bool
	for i, q := range state.queues {
		depths[i] = q.len()
		depthChanged[i] = depths[i] != state.lastDepth[i]
		state.lastDepth[i] = depths[i]
	}
	state.queueMu.Unlock()

	for _, class := range drainClasses {
		if depthChanged[class] {
			nsm.tel.recordQueueDepth(class, nodeID, depths[class])
		}
	}
	return out
}

func (nsm *NodeStateManager) GetMessageNotifier(nodeID cluster.NodeID) <-chan struct{} {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		// This should not happen in the new design, as control loops are only
		// created for managed nodes. Returning nil is the safe fallback.
		nsm.logger.Error("GetMessageNotifier called for unmanaged node", zap.String("node_id", nodeID))
		return nil
	}
	return state.messageNotify
}

// RequeueMessages returns previously-extracted Outbound entries to the
// head of the per-class queue that originally produced them. Each entry's
// class is honored individually so a mixed-class drain can be requeued
// without losing QoS context. Internally splits the input by class and
// delegates to RequeueMessagesClass for the per-class cap arithmetic.
func (nsm *NodeStateManager) RequeueMessages(nodeID cluster.NodeID, messages []Outbound) {
	if len(messages) == 0 {
		return
	}
	var perClass [numClasses][][]byte
	for _, m := range messages {
		if m.Data == nil {
			continue
		}
		if int(m.Class) >= numClasses {
			continue
		}
		perClass[m.Class] = append(perClass[m.Class], m.Data)
	}
	for c := 0; c < numClasses; c++ {
		if len(perClass[c]) == 0 {
			continue
		}
		nsm.RequeueMessagesClass(nodeID, perClass[c], Class(c))
	}
}

// RequeueMessagesClass returns previously-extracted messages to the head
// of the per-class queue. Reliable classes are preserved while the peer
// remains managed. Gossip keeps its lossy cap.
func (nsm *NodeStateManager) RequeueMessagesClass(nodeID cluster.NodeID, messages [][]byte, class Class) {
	if len(messages) == 0 {
		return
	}
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		nsm.logger.Warn("Dropping messages to requeue for unmanaged node",
			zap.String("node_id", nodeID),
			zap.Int("message_count", len(messages)),
			zap.String("class", class.String()))
		return
	}
	if int(class) >= numClasses {
		return
	}

	state.queueMu.Lock()
	q := state.queues[class]
	dropped := 0
	switch class {
	case ClassRaftControl, ClassPGBroadcast, ClassRaftRPC:
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i] == nil {
				continue
			}
			q.pushFront(messages[i])
		}
	case ClassGossip:
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i] == nil {
				continue
			}
			if !q.pushFront(messages[i]) {
				dropped++
			}
		}
	}
	depth := q.len()
	depthChanged := depth != state.lastDepth[class]
	state.lastDepth[class] = depth
	state.queueMu.Unlock()

	for i := 0; i < dropped; i++ {
		nsm.tel.recordDrop(class, "requeue_overflow")
	}
	if depthChanged {
		nsm.tel.recordQueueDepth(class, nodeID, depth)
	}

	if dropped > 0 {
		nsm.logger.Warn("Dropped messages during requeue (queue full)",
			zap.String("node_id", nodeID),
			zap.String("class", class.String()),
			zap.Int("dropped", dropped))
	}

	select {
	case state.messageNotify <- struct{}{}:
	default:
	}
}

// RemoveNodeState completely removes a node's state from memory.
// This should only be called by the manager when a node leaves the cluster.
func (nsm *NodeStateManager) RemoveNodeState(nodeID cluster.NodeID) {
	state, ok := nsm.nodeStates.LoadAndDelete(nodeID)
	if !ok {
		return
	}

	nsm.logger.Info("Removing managed state for node", zap.String("node", nodeID))
	nodeState := state.(*NodeState)

	nodeState.stateMu.Lock()
	if nodeState.connection != nil {
		nodeState.connection.Close()
		nodeState.connection = nil
	}
	nodeState.stateMu.Unlock()

	nodeState.queueMu.Lock()
	discarded := 0
	for _, q := range nodeState.queues {
		discarded += q.len()
		q.reset()
	}
	nodeState.queueMu.Unlock()

	if discarded > 0 {
		nsm.logger.Warn("Discarded pending messages for removed node",
			zap.String("node", nodeID),
			zap.Int("discarded_messages", discarded))
	}
}

func (nsm *NodeStateManager) GetConnectedNodes() []cluster.NodeID {
	var connected []cluster.NodeID
	nsm.nodeStates.Range(func(key, value any) bool {
		nodeID := key.(cluster.NodeID)
		state := value.(*NodeState)
		state.stateMu.RLock()
		isConnected := state.state == StateConnected
		state.stateMu.RUnlock()
		if isConnected {
			connected = append(connected, nodeID)
		}
		return true
	})
	return connected
}
