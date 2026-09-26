// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
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
	session       *session // guarded by queueMu; replaced when the session ends
	// departure is closed once the removal of this state has been signaled.
	departure chan struct{}
	// retired lists peer incarnations whose sessions ended because the peer
	// restarted; a late connection from one is rejected. Guarded by queueMu.
	retired     []uint64
	queueMu     queueMutex
	address     nodeAddress
	lastDepth   [numClasses]int // last queue depth emitted to telemetry; guarded by queueMu
	surfaceTurn bool            // guarded by queueMu; fair turns between application classes
	state       ConnectionState
	stateMu     sync.RWMutex
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

// peekLen returns the size of the oldest entry; ok=false when empty.
func (q *classQueue) peekLen() (size int, ok bool) {
	if q.size == 0 {
		return 0, false
	}
	return len(q.buf[q.head]), true
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
	// sessionEnded receives every ended session once its last frame was
	// delivered. Set by the manager before it serves peers.
	sessionEnded func(cluster.NodeID)
	// departed maps a removed node to its state's departure channel; a later
	// state for the node delivers nothing before it closes.
	departed sync.Map // cluster.NodeID -> <-chan struct{}
	config   ManagerConfig
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

		// Reset all queues and the session.
		oldState.queueMu.Lock()
		for i := range oldState.queues {
			oldState.queues[i].reset()
		}
		end := endSessionLocked(oldState, sessionEndRemoved, 0, true)
		oldState.retired = nil
		oldState.queueMu.Unlock()
		nsm.finishSessionEnd(nodeID, end)

		// Do NOT replace messageNotify — existing control loops hold a reference.
		nsm.logger.Debug("Reset existing state for rejoining node", zap.String("node_id", nodeID))
		return
	}

	caps := [numClasses]int{
		ClassRaftControl: 0,
		ClassGossip:      nsm.config.GossipQueueCap,
		ClassPGBroadcast: 0,
		ClassRaftRPC:     0,
		ClassSurface:     surfaceQueueCap,
	}
	queues := [numClasses]*classQueue{}
	for i := range queues {
		queues[i] = newClassQueue(caps[i])
	}
	newState := &NodeState{
		queues:        queues,
		session:       newSession(0, nsm.predecessorOf(nodeID)),
		departure:     make(chan struct{}),
		messageNotify: make(chan struct{}, 1),
		state:         StateNone,
		createdAt:     time.Now(),
	}
	nsm.nodeStates.Store(nodeID, newState)
}

// predecessorOf returns the settle channel of the removed node's last
// session while its end is still being signaled.
func (nsm *NodeStateManager) predecessorOf(nodeID cluster.NodeID) <-chan struct{} {
	if settled, ok := nsm.departed.Load(nodeID); ok {
		return settled.(<-chan struct{})
	}
	return nil
}

func (nsm *NodeStateManager) GetNodeState(nodeID cluster.NodeID) *NodeState {
	state, ok := nsm.nodeStates.Load(nodeID)
	if !ok {
		return nil
	}
	return state.(*NodeState)
}

// QueueMessageClass enqueues data for nodeID under the given class.
// Admission policy is class-specific:
//   - ClassRaftControl, ClassPGBroadcast, and ClassRaftRPC are unbounded
//     while the peer remains managed.
//   - ClassGossip and ClassSurface reject the new entry and return ErrQueueFull when full.
//
// In all drop cases, internode_dropped_total{class,reason="queue_full"}
// is incremented.
//
// Returns ErrNodeNotManaged if no state exists for nodeID.
// Returns ErrQueueFull for gossip or surface traffic when full.
func (nsm *NodeStateManager) QueueMessageClass(nodeID cluster.NodeID, data []byte, class Class) error {
	return nsm.queueMessageClass(context.Background(), nodeID, data, class, false)
}

// QueueMessageClassContext cancels queue-lock admission, not accepted delivery.
func (nsm *NodeStateManager) QueueMessageClassContext(ctx context.Context, nodeID cluster.NodeID, data []byte, class Class) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return nsm.queueMessageClass(ctx, nodeID, data, class, true)
}

func (nsm *NodeStateManager) queueMessageClass(ctx context.Context, nodeID cluster.NodeID, data []byte, class Class, cancellable bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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

	if class == ClassSurface && len(data) > MaxSurfaceFrameSize {
		return NewMessageSizeExceedsMaxError(len(data), MaxSurfaceFrameSize)
	}
	if cancellable {
		if err := state.queueMu.LockContext(ctx); err != nil {
			return err
		}
	} else {
		state.queueMu.Lock()
	}
	if nsm.GetNodeState(nodeID) != state {
		state.queueMu.Unlock()
		return ErrNodeNotManaged
	}
	q := state.queues[class]
	var rejected bool
	switch class {
	case ClassRaftControl, ClassPGBroadcast, ClassRaftRPC:
		q.pushNewest(data)
	case ClassSurface, ClassGossip:
		rejected = !q.pushNewest(data)
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
	nsm.setNodeConnectionForState(nodeID, state, conn, newState)
}

// setNodeConnectionForState updates only the supplied generation. A control
// loop retains this pointer so cleanup from an old incarnation cannot modify
// a replacement that has reused the same node ID.
func (nsm *NodeStateManager) setNodeConnectionForState(nodeID cluster.NodeID, state *NodeState, conn *NodeConnection, newState ConnectionState) bool {
	if state == nil || nsm.GetNodeState(nodeID) != state {
		return false
	}
	state.stateMu.Lock()
	state.connection = conn
	state.state = newState
	state.stateMu.Unlock()
	return true
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
	nsm.setNodeStateForState(nodeID, state, newState)
}

func (nsm *NodeStateManager) setNodeStateForState(nodeID cluster.NodeID, state *NodeState, newState ConnectionState) bool {
	if state == nil || nsm.GetNodeState(nodeID) != state {
		return false
	}
	state.stateMu.Lock()
	state.state = newState
	state.stateMu.Unlock()
	return true
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
	return nsm.getNodeAddressForState(nodeID, state)
}

func (nsm *NodeStateManager) getNodeAddressForState(nodeID cluster.NodeID, state *NodeState) (string, int, bool) {
	if state == nil || nsm.GetNodeState(nodeID) != state {
		return "", 0, false
	}
	state.stateMu.RLock()
	addr := state.address
	state.stateMu.RUnlock()

	return addr.addr, addr.port, addr.addr != "" && addr.port != 0
}

// Control traffic retains its existing priority. Surface and PG application
// frames then take alternating turns so adding interactive traffic cannot
// starve ordinary process messages (including when maxCount is one).
var drainClasses = [...]Class{ClassRaftControl, ClassRaftRPC, ClassGossip}

// DrainMessages hands up to maxCount frames of nodeID's current session to a
// writer, sequencing them in drain order.
func (nsm *NodeStateManager) DrainMessages(nodeID cluster.NodeID, maxCount int) []Outbound {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return nil
	}
	state.queueMu.Lock()
	sess := state.session
	state.queueMu.Unlock()
	return nsm.drainSession(nodeID, state, sess, maxCount)
}

// drainSession hands frames of one session generation to its writer. Frames
// awaiting retransmission go first, in sequence order; queued frames follow
// in QoS order. Each sequenced frame takes the next sequence number and
// enters the resend ring, so wire order equals sequence order. While the
// ring holds a window of unacknowledged bytes, sequenced classes stay queued;
// gossip is unsequenced and keeps flowing. A stale connection bound to an
// ended session or a detached state drains nothing.
func (nsm *NodeStateManager) drainSession(nodeID cluster.NodeID, state *NodeState, sess *session, maxCount int) []Outbound {
	if state == nil || nsm.GetNodeState(nodeID) != state || maxCount <= 0 {
		return nil
	}

	state.queueMu.Lock()
	if state.session != sess {
		state.queueMu.Unlock()
		return nil
	}
	out := make([]Outbound, 0, maxCount)
	for len(out) < maxCount {
		e, ok := sess.ring.next()
		if !ok {
			break
		}
		out = append(out, Outbound{Data: e.data, Class: e.class, seq: e.seq})
	}
	window := nsm.config.LinkWindowBytes
	take := func(class Class) bool {
		q := state.queues[class]
		size, ok := q.peekLen()
		if !ok || (class.sequenced() && !sess.ring.admits(size, window)) {
			return false
		}
		data, _ := q.pop()
		frame := Outbound{Data: data, Class: class}
		if class.sequenced() {
			frame.seq = sess.sendNext
			sess.sendNext++
			sess.ring.push(ringEntry{data: data, seq: frame.seq, class: class})
		}
		out = append(out, frame)
		return true
	}
	for _, class := range drainClasses {
		for len(out) < maxCount {
			if !take(class) {
				break
			}
		}
	}
	surfaceCount := 0
	for len(out) < maxCount {
		first, second := ClassPGBroadcast, ClassSurface
		if state.surfaceTurn {
			first, second = second, first
		}
		class := first
		if (class == ClassSurface && surfaceCount == surfaceQueueCap) || !take(class) {
			class = second
			if (class == ClassSurface && surfaceCount == surfaceQueueCap) || !take(class) {
				break
			}
		}
		if class == ClassSurface {
			surfaceCount++
		}
		state.surfaceTurn = class != ClassSurface
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

	for class := range numClasses {
		if depthChanged[class] {
			nsm.tel.recordQueueDepth(Class(class), nodeID, depths[class])
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

// RemoveNodeState completely removes a node's state from memory.
// This should only be called by the manager when a node leaves the cluster.
func (nsm *NodeStateManager) RemoveNodeState(nodeID cluster.NodeID) {
	nsm.closeDetachedNodeState(nodeID, nsm.detachNodeState(nodeID))
}

// Separate identity removal from resource cleanup so a manager can serialize
// join/leave decisions without holding its lifecycle lock during Close.
func (nsm *NodeStateManager) detachNodeState(nodeID cluster.NodeID) *NodeState {
	loaded, ok := nsm.nodeStates.Load(nodeID)
	if !ok {
		return nil
	}
	state := loaded.(*NodeState)
	if !nsm.detach(nodeID, state) {
		return nil
	}
	return state
}

// detach removes state from the node map and makes its departure the
// predecessor of any later state for the node.
func (nsm *NodeStateManager) detach(nodeID cluster.NodeID, state *NodeState) bool {
	if !nsm.nodeStates.CompareAndDelete(nodeID, state) {
		return false
	}
	nsm.departed.Store(nodeID, (<-chan struct{})(state.departure))
	return true
}

func (nsm *NodeStateManager) closeDetachedNodeState(nodeID cluster.NodeID, nodeState *NodeState) {
	if nodeState == nil {
		return
	}
	nsm.logger.Info("Removing managed state for node", zap.String("node", nodeID))
	nodeState.stateMu.Lock()
	if nodeState.connection != nil {
		nodeState.connection.Close()
		nodeState.connection = nil
	}
	nodeState.stateMu.Unlock()

	nodeState.queueMu.Lock()
	// The removed state's last session carries the state's departure.
	nodeState.session.alsoSettles = nodeState.departure
	end := endSessionLocked(nodeState, sessionEndRemoved, nodeState.session.peerIncarnation, true)
	nodeState.queueMu.Unlock()
	nsm.finishSessionEnd(nodeID, end)
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
