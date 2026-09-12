// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"errors"
	"fmt"
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

// Generation identity and retained capacity follow the same queue lifetime.
type queueGeneration struct{ reliable *outboundBudget }

type NodeState struct {
	reliable      *outboundBudget  // immutable peer lifetime; closes without queueMu
	generation    *queueGeneration // guarded by queueMu
	createdAt     time.Time        // for observability: when this state was first created
	queues        [numClasses]*classQueue
	messageNotify chan struct{}
	connection    *NodeConnection
	queueMu       queueMutex
	address       nodeAddress
	lastDepth     [numClasses]int // last queue depth emitted to telemetry; guarded by queueMu
	surfaceTurn   bool            // guarded by queueMu; fair turns between application classes
	state         ConnectionState
	stateMu       sync.RWMutex
}

// classQueue is a FIFO of pending messages for one Class.
// All access is guarded by NodeState.queueMu (held for cross-class
// operations). A zero capacity is unbounded.
type classQueue struct {
	buf       []Outbound
	head      int // index of the oldest element (next to drain)
	size      int // number of valid entries
	limit     int // bounded capacity, independent of currently allocated storage
	unbounded bool
}

func newClassQueue(cap int) *classQueue {
	if cap <= 0 {
		return &classQueue{unbounded: true}
	}
	return &classQueue{limit: cap}
}

// reserveBoundedSlot grows storage only on demand. A full ring is copied in
// logical FIFO order, including wrapped heads; the configured cap never grows.
func (q *classQueue) reserveBoundedSlot() bool {
	if q.size == q.limit {
		return false
	}
	if q.size < len(q.buf) {
		return true
	}
	capacity := min(q.limit, 8)
	if len(q.buf) != 0 {
		capacity = len(q.buf) + min(len(q.buf), q.limit-len(q.buf))
	}
	storage := make([]Outbound, capacity)
	if q.size != 0 {
		copied := copy(storage, q.buf[q.head:])
		copy(storage[copied:], q.buf[:q.head])
	}
	q.buf, q.head = storage, 0
	return true
}

// pushNewest appends if there is room. Returns false if full (no insert).
func (q *classQueue) pushNewest(data []byte) bool { return q.pushFrame(Outbound{Data: data}) }
func (q *classQueue) pushFrame(data Outbound) (accepted bool) {
	if q.unbounded {
		q.buf = append(q.buf, data)
		q.size++
		return true
	}
	if !q.reserveBoundedSlot() {
		return false
	}
	tail := (q.head + q.size) % len(q.buf)
	q.buf[tail] = data
	q.size++
	return true
}

// pushFront inserts at the front for requeue (callers must respect cap).
// Returns false if full.
func (q *classQueue) pushFront(data []byte) bool { return q.pushFrontFrame(Outbound{Data: data}) }
func (q *classQueue) pushFrontFrame(data Outbound) (accepted bool) {
	if q.unbounded {
		if q.head > 0 {
			q.head--
			q.buf[q.head] = data
		} else {
			q.buf = append(q.buf, Outbound{})
			copy(q.buf[1:], q.buf)
			q.buf[0] = data
		}
		q.size++
		return true
	}
	if !q.reserveBoundedSlot() {
		return false
	}
	q.head = (q.head - 1 + len(q.buf)) % len(q.buf)
	q.buf[q.head] = data
	q.size++
	return true
}

// pop removes and returns the oldest entry; ok=false when empty.
func (q *classQueue) pop() (data Outbound, ok bool) {
	if q.size == 0 {
		return Outbound{}, false
	}
	if q.unbounded {
		data = q.buf[q.head]
		q.buf[q.head] = Outbound{}
		q.head++
		q.size--
		if q.size == 0 {
			q.head = 0
			q.buf = q.buf[:0]
		} else if q.head > 1024 && q.head*2 >= len(q.buf) {
			copy(q.buf, q.buf[q.head:])
			for i := q.size; i < len(q.buf); i++ {
				q.buf[i] = Outbound{}
			}
			q.buf = q.buf[:q.size]
			q.head = 0
		}
		return data, true
	}
	data = q.buf[q.head]
	q.buf[q.head] = Outbound{} // release reference
	q.head = (q.head + 1) % len(q.buf)
	q.size--
	return data, true
}

// reset drops all entries. Allocations remain.
func (q *classQueue) reset() {
	for i := range q.buf {
		q.buf[i].reservation.release()
		q.buf[i] = Outbound{}
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
	reliable   *outboundBudget
	budgetErr  error
	nodeStates sync.Map // cluster.NodeID -> *NodeState
	logger     *zap.Logger
	tel        *telemetry
	config     ManagerConfig
}

func NewNodeStateManager(config ManagerConfig, tel *telemetry, logger *zap.Logger) *NodeStateManager {
	defaults := DefaultManagerConfig()
	if config.DrainBatchBytes == 0 {
		config.DrainBatchBytes = defaults.DrainBatchBytes
	}
	if config.OutboundQueueSize == 0 {
		config.OutboundQueueSize = defaults.OutboundQueueSize
	}
	if config.OutboundPeerBytes == 0 {
		config.OutboundPeerBytes = defaults.OutboundPeerBytes
	}
	if config.OutboundTotalBytes == 0 {
		config.OutboundTotalBytes = defaults.OutboundTotalBytes
	}
	if config.OutboundTotalEntries == 0 {
		config.OutboundTotalEntries = defaults.OutboundTotalEntries
	}
	root, err := newOutboundBudget(config.OutboundTotalEntries, config.OutboundTotalBytes)
	if config.OutboundQueueSize < 0 {
		err = fmt.Errorf("outbound queue size must be positive")
	}
	if err == nil {
		if config.OutboundControlPeerEntries >= uint64(config.OutboundQueueSize) || config.OutboundControlPeerBytes >= config.OutboundPeerBytes {
			err = fmt.Errorf("outbound peer control reserve must leave ordinary capacity")
		} else if protectErr := root.protect(config.OutboundControlTotalEntries, config.OutboundControlTotalBytes); protectErr != nil {
			err = fmt.Errorf("outbound total control reserve: %w", protectErr)
		}
	}
	return &NodeStateManager{
		reliable: root, budgetErr: err,
		logger: logger.Named("state"),
		tel:    tel,
		config: config,
	}
}

func (nsm *NodeStateManager) newQueueGeneration(parent *outboundBudget) *queueGeneration {
	budget, _ := parent.child(uint64(nsm.config.OutboundQueueSize), nsm.config.OutboundPeerBytes)
	if nsm.budgetErr == nil {
		_ = budget.protect(nsm.config.OutboundControlPeerEntries, nsm.config.OutboundControlPeerBytes)
	}
	return &queueGeneration{reliable: budget}
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

		// Reset all queues
		oldState.queueMu.Lock()
		oldState.generation.reliable.close()
		oldState.generation = nsm.newQueueGeneration(oldState.reliable)
		for i := range oldState.queues {
			oldState.queues[i].reset()
		}
		oldState.lastDepth = [numClasses]int{}
		oldState.queueMu.Unlock()

		// Reset connection
		oldState.stateMu.Lock()
		if oldState.connection != nil {
			oldState.connection.Close()
			oldState.connection = nil
		}
		oldState.state = StateNone
		oldState.address = nodeAddress{}
		oldState.stateMu.Unlock()

		// Do NOT replace messageNotify — existing control loops hold a reference.
		nsm.logger.Debug("Reset existing state for rejoining node", zap.String("node_id", nodeID))
		return
	}

	caps := [numClasses]int{
		ClassRaftControl: 0,
		ClassGossip:      nsm.config.GossipQueueCap,
		ClassPGBroadcast: 0,
		ClassRaftRPC:     0,
		// Admission and drain each allow 32 surface frames. Reserve another
		// batch so a failed write can requeue every accepted frame.
		ClassSurface: 64,
	}
	queues := [numClasses]*classQueue{}
	for i := range queues {
		queues[i] = newClassQueue(caps[i])
	}
	peerBudget, _ := nsm.reliable.child(uint64(nsm.config.OutboundQueueSize), nsm.config.OutboundPeerBytes)
	if nsm.budgetErr == nil {
		_ = peerBudget.protect(nsm.config.OutboundControlPeerEntries, nsm.config.OutboundControlPeerBytes)
	}
	newState := &NodeState{
		reliable:      peerBudget,
		queues:        queues,
		generation:    nsm.newQueueGeneration(peerBudget),
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
//     after admission while the peer remains managed. Admission is bounded.
//   - ClassGossip and ClassSurface reject the new entry and returns ErrQueueFull when full.
//
// In all drop cases, internode_dropped_total{class,reason="queue_full"}
// is incremented.
//
// Returns ErrNodeNotManaged if no state exists for nodeID.
// Returns ErrQueueFull before ownership transfer when the applicable budget is full.
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
	generation := state.generation
	var reservation *outboundReservation
	reliable := class == ClassRaftControl || class == ClassRaftRPC || class == ClassPGBroadcast
	if reliable {
		state.queueMu.Unlock()
		if nsm.budgetErr != nil {
			return nsm.budgetErr
		}
		var err error
		reservation, err = generation.reliable.reserveTraffic(ctx, uint64(len(data)), cancellable, class == ClassRaftControl || class == ClassRaftRPC)
		if err != nil {
			if errors.Is(err, ErrQueueFull) {
				nsm.tel.recordDrop(class, "queue_full")
			}
			return err
		}
		if err = state.queueMu.LockContext(ctx); err != nil {
			reservation.release()
			return err
		}
		if nsm.GetNodeState(nodeID) != state || state.generation != generation {
			state.queueMu.Unlock()
			reservation.release()
			return ErrNodeNotManaged
		}
	}
	q := state.queues[class]
	var rejected bool
	switch class {
	case ClassRaftControl, ClassPGBroadcast, ClassRaftRPC:
		q.pushFrame(Outbound{Data: data, reservation: reservation, generation: generation})
	case ClassSurface:
		if q.len() >= 32 {
			rejected = true
		} else {
			q.pushNewest(data)
		}
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

func (nsm *NodeStateManager) DrainMessages(nodeID cluster.NodeID, maxCount int) []Outbound {
	state := nsm.GetNodeState(nodeID)
	return nsm.drainMessagesForState(nodeID, state, maxCount)
}

// drainMessagesForState drains only the supplied generation. Connection drain
// callbacks retain their loop's state pointer, preventing a detached
// connection from consuming a replacement's queue.
func (nsm *NodeStateManager) drainMessagesForState(nodeID cluster.NodeID, state *NodeState, maxCount int) []Outbound {
	if state == nil {
		return nil
	}
	state.queueMu.Lock()
	generation := state.generation
	state.queueMu.Unlock()
	return nsm.drainMessagesForGeneration(nodeID, state, generation, maxCount)
}

func (nsm *NodeStateManager) drainMessagesForGeneration(nodeID cluster.NodeID, state *NodeState, generation *queueGeneration, maxCount int) []Outbound {
	if state == nil || maxCount <= 0 {
		return nil
	}
	state.queueMu.Lock()
	if nsm.GetNodeState(nodeID) != state || state.generation != generation {
		state.queueMu.Unlock()
		return nil
	}
	out := make([]Outbound, 0, maxCount)
	var batchBytes uint64
	batchFull := false
	fits := func(q *classQueue) bool {
		// One already-admitted oversized frame must make progress. The batch
		// target bounds additional frames; it does not change message-size limits.
		if len(out) == 0 {
			return true
		}
		return batchBytes < nsm.config.DrainBatchBytes && uint64(len(q.buf[q.head].Data)) <= nsm.config.DrainBatchBytes-batchBytes
	}
	for _, class := range drainClasses {
		q := state.queues[class]
		for q.len() > 0 && len(out) < maxCount {
			if !fits(q) {
				batchFull = true
				break
			}
			d, _ := q.pop()
			if d.Data != nil {
				d.Class = class
				d.generation = generation
				out = append(out, d)
				batchBytes += uint64(len(d.Data))
			} else {
				d.reservation.release()
			}
		}
		if batchFull || len(out) >= maxCount {
			break
		}
	}
	surfaceCount := 0
	for !batchFull && len(out) < maxCount {
		first, second := ClassPGBroadcast, ClassSurface
		if state.surfaceTurn {
			first, second = second, first
		}
		class := first
		if state.queues[class].len() == 0 || (class == ClassSurface && surfaceCount == 32) {
			class = second
		}
		if state.queues[class].len() == 0 || (class == ClassSurface && surfaceCount == 32) {
			break
		}
		if !fits(state.queues[class]) {
			break
		}
		data, _ := state.queues[class].pop()
		if data.Data != nil {
			data.Class = class
			data.generation = generation
			out = append(out, data)
			batchBytes += uint64(len(data.Data))
		} else {
			data.reservation.release()
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

// RequeueMessages returns previously-extracted Outbound entries to the
// head of the per-class queue that originally produced them. Each entry's
// class is honored individually so a mixed-class drain can be requeued
// without losing QoS context. Internally splits the input by class and
// delegates to RequeueMessagesClass for the per-class cap arithmetic.
func (nsm *NodeStateManager) RequeueMessages(nodeID cluster.NodeID, messages []Outbound) {
	state := nsm.GetNodeState(nodeID)
	nsm.requeueMessagesForState(nodeID, state, messages)
}

// requeueMessagesForState returns frames only to the supplied generation.
// A stale connection must never repopulate a newer peer incarnation's queue.
func (nsm *NodeStateManager) requeueMessagesForState(nodeID cluster.NodeID, state *NodeState, messages []Outbound) {
	if state == nil {
		releaseOutbound(messages)
		return
	}
	state.queueMu.Lock()
	if nsm.GetNodeState(nodeID) != state {
		state.queueMu.Unlock()
		releaseOutbound(messages)
		return
	}
	var drops [numClasses]int
	for i := len(messages) - 1; i >= 0; i-- {
		frame := messages[i]
		if frame.Data == nil || int(frame.Class) >= numClasses || (frame.generation != nil && frame.generation != state.generation) {
			frame.reservation.release()
			continue
		}
		if !state.queues[frame.Class].pushFrontFrame(frame) {
			frame.reservation.release()
			drops[frame.Class]++
		}
	}
	var depths [numClasses]int
	var changed [numClasses]bool
	for class, q := range state.queues {
		depths[class] = q.len()
		changed[class] = depths[class] != state.lastDepth[class]
		state.lastDepth[class] = depths[class]
	}
	state.queueMu.Unlock()
	for class := range numClasses {
		for range drops[class] {
			nsm.tel.recordDrop(Class(class), "requeue_overflow")
		}
		if changed[class] {
			nsm.tel.recordQueueDepth(Class(class), nodeID, depths[class])
		}
	}
	select {
	case state.messageNotify <- struct{}{}:
	default:
	}
}

// RequeueMessagesClass is the raw-frame adapter; reserved writer batches must
// use RequeueMessages so their retained-byte ownership follows the batch.
func (nsm *NodeStateManager) RequeueMessagesClass(nodeID cluster.NodeID, messages [][]byte, class Class) {
	nsm.requeueMessagesClassForState(nodeID, nsm.GetNodeState(nodeID), messages, class)
}
func (nsm *NodeStateManager) requeueMessagesClassForState(nodeID cluster.NodeID, state *NodeState, messages [][]byte, class Class) {
	frames := make([]Outbound, len(messages))
	for i, data := range messages {
		frames[i] = Outbound{Data: data, Class: class}
	}
	nsm.requeueMessagesForState(nodeID, state, frames)
}

// RemoveNodeState completely removes a node's state from memory.
// This should only be called by the manager when a node leaves the cluster.
func (nsm *NodeStateManager) RemoveNodeState(nodeID cluster.NodeID) {
	nsm.closeDetachedNodeState(nodeID, nsm.detachNodeState(nodeID))
}

// Separate identity removal from resource cleanup so a manager can serialize
// join/leave decisions without holding its lifecycle lock during Close.
func (nsm *NodeStateManager) detachNodeState(nodeID cluster.NodeID) *NodeState {
	state, ok := nsm.nodeStates.LoadAndDelete(nodeID)
	if !ok {
		return nil
	}
	node := state.(*NodeState)
	node.reliable.close()
	return node
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
