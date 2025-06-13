package internode

import (
	"container/list"
	"sync"
	"sync/atomic"

	"github.com/ponyruntime/pony/api/cluster"
	"go.uber.org/zap"
)

// nodeAddress holds addressing info atomically
type nodeAddress struct {
	addr string
	port int
}

// NodeState tracks the state and message queue for a single remote node
type NodeState struct {
	// Persistent message queue (survives connection failures) - KEEP UNBOUNDED!
	messageQueue *list.List // [][]byte messages in send order
	queueMu      sync.Mutex

	// Message notification for efficient draining (non-blocking)
	messageNotify chan struct{}

	// Connection management - use atomics for lock-free reads
	connection atomic.Pointer[NodeConnection]
	state      atomic.Int32 // ConnectionState as int32

	// Node addressing - atomic for lock-free reads
	address atomic.Value // stores nodeAddress

	// Coordination - keep mutex as it's not hot path
	isConnecting sync.Mutex
}

// NodeStateManager manages the state and message queues for all remote nodes.
type NodeStateManager struct {
	// Node state tracking - use sync.Map for better concurrent access
	nodeStates sync.Map // cluster.NodeID -> *NodeState

	logger *zap.Logger
	config ManagerConfig
}

// NewNodeStateManager creates a new NodeStateManager.
func NewNodeStateManager(config ManagerConfig, logger *zap.Logger) *NodeStateManager {
	return &NodeStateManager{
		logger: logger.Named("node"),
		config: config,
	}
}

// GetOrCreateNodeState returns the NodeState for a given node, creating it if necessary.
func (nsm *NodeStateManager) GetOrCreateNodeState(nodeID cluster.NodeID) *NodeState {
	if state, ok := nsm.nodeStates.Load(nodeID); ok {
		return state.(*NodeState)
	}

	// Create new state
	newState := &NodeState{
		messageQueue:  list.New(),
		messageNotify: make(chan struct{}, 1), // Buffered to prevent blocking
	}
	newState.state.Store(int32(StateNone))

	// Try to store, return existing if another goroutine created it first
	if actual, loaded := nsm.nodeStates.LoadOrStore(nodeID, newState); loaded {
		return actual.(*NodeState)
	}

	return newState
}

// GetNodeState returns the NodeState for a given node, or nil if it doesn't exist.
func (nsm *NodeStateManager) GetNodeState(nodeID cluster.NodeID) *NodeState {
	if state, ok := nsm.nodeStates.Load(nodeID); ok {
		return state.(*NodeState)
	}
	return nil
}

// QueueMessage adds a message to the specified node's queue.
// This operation never fails (Erlang semantics) - UNBOUNDED queue.
func (nsm *NodeStateManager) QueueMessage(nodeID cluster.NodeID, data []byte) {
	state := nsm.GetOrCreateNodeState(nodeID)

	state.queueMu.Lock()
	state.messageQueue.PushBack(data)
	state.queueMu.Unlock()

	// Notify drainer that messages are available (non-blocking)
	select {
	case state.messageNotify <- struct{}{}:
	default:
		// Channel already has notification, no need to send another
	}
}

// SetNodeConnection updates the connection for a node using atomics.
func (nsm *NodeStateManager) SetNodeConnection(nodeID cluster.NodeID, conn *NodeConnection, newState ConnectionState) {
	state := nsm.GetOrCreateNodeState(nodeID)

	state.connection.Store(conn)
	state.state.Store(int32(newState))
}

// GetNodeConnection returns the current connection and state for a node using atomics.
func (nsm *NodeStateManager) GetNodeConnection(nodeID cluster.NodeID) (*NodeConnection, ConnectionState) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return nil, StateNone
	}

	conn := state.connection.Load()
	stateVal := ConnectionState(state.state.Load())

	return conn, stateVal
}

// SetNodeState updates the connection state for a node using atomics.
func (nsm *NodeStateManager) SetNodeState(nodeID cluster.NodeID, newState ConnectionState) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return
	}

	state.state.Store(int32(newState))
}

// UpdateNodeAddress updates the addressing information for a node using atomics.
func (nsm *NodeStateManager) UpdateNodeAddress(nodeID cluster.NodeID, addr string, port int) {
	state := nsm.GetOrCreateNodeState(nodeID)
	state.address.Store(nodeAddress{addr: addr, port: port})
}

// GetNodeAddress returns the addressing information for a node using atomics.
func (nsm *NodeStateManager) GetNodeAddress(nodeID cluster.NodeID) (string, int, bool) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return "", 0, false
	}

	if addr, ok := state.address.Load().(nodeAddress); ok {
		return addr.addr, addr.port, addr.addr != "" && addr.port != 0
	}

	return "", 0, false
}

// TryLockConnection attempts to acquire the connection lock for a node.
func (nsm *NodeStateManager) TryLockConnection(nodeID cluster.NodeID) bool {
	state := nsm.GetOrCreateNodeState(nodeID)
	return state.isConnecting.TryLock()
}

// UnlockConnection releases the connection lock for a node.
func (nsm *NodeStateManager) UnlockConnection(nodeID cluster.NodeID) {
	state := nsm.GetNodeState(nodeID)
	if state != nil {
		state.isConnecting.Unlock()
	}
}

// GetNextMessage retrieves and removes the next message from a node's queue.
// Returns nil if the queue is empty.
func (nsm *NodeStateManager) GetNextMessage(nodeID cluster.NodeID) []byte {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return nil
	}

	state.queueMu.Lock()
	defer state.queueMu.Unlock()

	elem := state.messageQueue.Front()
	if elem == nil {
		return nil
	}

	data, ok := elem.Value.([]byte)
	if !ok {
		state.messageQueue.Remove(elem)
		return nil
	}

	state.messageQueue.Remove(elem)
	return data
}

// DrainMessages retrieves multiple messages at once to reduce mutex contention.
func (nsm *NodeStateManager) DrainMessages(nodeID cluster.NodeID, maxCount int) [][]byte {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return nil
	}

	state.queueMu.Lock()
	defer state.queueMu.Unlock()

	var messages [][]byte
	for i := 0; i < maxCount && state.messageQueue.Len() > 0; i++ {
		elem := state.messageQueue.Front()
		if elem == nil {
			break
		}

		if data, ok := elem.Value.([]byte); ok {
			messages = append(messages, data)
			state.messageQueue.Remove(elem)
		} else {
			state.messageQueue.Remove(elem)
		}
	}

	return messages
}

// GetMessageNotifier returns the notification channel for a node.
func (nsm *NodeStateManager) GetMessageNotifier(nodeID cluster.NodeID) <-chan struct{} {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return nil
	}
	return state.messageNotify
}

// GetQueueLength returns the current queue length for a node.
func (nsm *NodeStateManager) GetQueueLength(nodeID cluster.NodeID) int {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return 0
	}

	state.queueMu.Lock()
	defer state.queueMu.Unlock()

	return state.messageQueue.Len()
}

// RequeueMessages adds messages to the front of a node's queue.
// Used when connection fails and we need to preserve message order.
func (nsm *NodeStateManager) RequeueMessages(nodeID cluster.NodeID, messages [][]byte) {
	if len(messages) == 0 {
		return
	}

	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return
	}

	state.queueMu.Lock()
	defer state.queueMu.Unlock()

	// Add messages to front in reverse order to preserve original order
	for i := len(messages) - 1; i >= 0; i-- {
		state.messageQueue.PushFront(messages[i])
	}

	// Notify drainer that messages are available (non-blocking)
	select {
	case state.messageNotify <- struct{}{}:
	default:
	}
}

// ClearNodeQueue clears all messages for a node (used when node leaves cluster).
func (nsm *NodeStateManager) ClearNodeQueue(nodeID cluster.NodeID) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return
	}

	state.queueMu.Lock()
	queueLen := state.messageQueue.Len()
	state.messageQueue = list.New()
	state.queueMu.Unlock()

	if queueLen > 0 {
		nsm.logger.Info("Cleared node message queue",
			zap.String("node", string(nodeID)),
			zap.Int("discarded_messages", queueLen))
	}
}

// CloseNodeConnection safely closes a node's connection and updates state.
func (nsm *NodeStateManager) CloseNodeConnection(nodeID cluster.NodeID) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return
	}

	if conn := state.connection.Load(); conn != nil {
		conn.Close()
		state.connection.Store(nil)
	}

	currentState := ConnectionState(state.state.Load())
	if currentState != StateDead {
		state.state.Store(int32(StateNone))
	}
}

// GetConnectedNodes returns a list of currently connected node IDs.
func (nsm *NodeStateManager) GetConnectedNodes() []cluster.NodeID {
	var connected []cluster.NodeID

	nsm.nodeStates.Range(func(key, value interface{}) bool {
		nodeID := key.(cluster.NodeID)
		state := value.(*NodeState)

		if ConnectionState(state.state.Load()) == StateConnected {
			connected = append(connected, nodeID)
		}
		return true
	})

	return connected
}

// GetAllNodeStates returns a snapshot of all node states for retry processing.
func (nsm *NodeStateManager) GetAllNodeStates() map[cluster.NodeID]ConnectionState {
	states := make(map[cluster.NodeID]ConnectionState)

	nsm.nodeStates.Range(func(key, value interface{}) bool {
		nodeID := key.(cluster.NodeID)
		state := value.(*NodeState)
		states[nodeID] = ConnectionState(state.state.Load())
		return true
	})

	return states
}

// MarkNodeDead marks a node as dead and clears its queue.
func (nsm *NodeStateManager) MarkNodeDead(nodeID cluster.NodeID) {
	nsm.logger.Info("Marking node as dead", zap.String("node", string(nodeID)))

	// Close connection
	nsm.CloseNodeConnection(nodeID)

	// Mark as dead
	nsm.SetNodeState(nodeID, StateDead)

	// Clear queue - ONLY place this happens
	nsm.ClearNodeQueue(nodeID)
}
