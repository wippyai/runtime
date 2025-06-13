package internode

import (
	"container/list"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/cluster"
	"go.uber.org/zap"
)

// NodeState tracks the state and message queue for a single remote node
type NodeState struct {
	// Persistent message queue (survives connection failures)
	messageQueue *list.List // [][]byte messages in send order
	queueMu      sync.Mutex

	// Connection management
	connection *NodeConnection
	state      ConnectionState
	stateMu    sync.RWMutex

	// Node addressing
	addr string
	port int

	// Coordination
	isConnecting sync.Mutex // Prevents multiple concurrent connection attempts
}

// NodeStateManager manages the state and message queues for all remote nodes.
// It provides thread-safe operations for node state management and message queuing.
type NodeStateManager struct {
	// Node state tracking - contains both connected and disconnected nodes
	nodeStates map[cluster.NodeID]*NodeState
	nodesMu    sync.RWMutex

	logger *zap.Logger
	config ManagerConfig
}

// NewNodeStateManager creates a new NodeStateManager.
func NewNodeStateManager(config ManagerConfig, logger *zap.Logger) *NodeStateManager {
	return &NodeStateManager{
		nodeStates: make(map[cluster.NodeID]*NodeState),
		logger:     logger.Named("node-state"),
		config:     config,
	}
}

// GetOrCreateNodeState returns the NodeState for a given node, creating it if necessary.
func (nsm *NodeStateManager) GetOrCreateNodeState(nodeID cluster.NodeID) *NodeState {
	nsm.nodesMu.RLock()
	state, exists := nsm.nodeStates[nodeID]
	nsm.nodesMu.RUnlock()

	if exists {
		return state
	}

	// Create new state
	nsm.nodesMu.Lock()
	defer nsm.nodesMu.Unlock()

	// Double-check after acquiring write lock
	if state, exists := nsm.nodeStates[nodeID]; exists {
		return state
	}

	state = &NodeState{
		messageQueue: list.New(),
		state:        StateNone,
	}
	nsm.nodeStates[nodeID] = state

	nsm.logger.Debug("Created new node state", zap.String("node", string(nodeID)))
	return state
}

// GetNodeState returns the NodeState for a given node, or nil if it doesn't exist.
func (nsm *NodeStateManager) GetNodeState(nodeID cluster.NodeID) *NodeState {
	nsm.nodesMu.RLock()
	defer nsm.nodesMu.RUnlock()
	return nsm.nodeStates[nodeID]
}

// QueueMessage adds a message to the specified node's queue.
// This operation never fails (Erlang semantics).
func (nsm *NodeStateManager) QueueMessage(nodeID cluster.NodeID, data []byte) {
	state := nsm.GetOrCreateNodeState(nodeID)

	state.queueMu.Lock()
	state.messageQueue.PushBack(data)
	queueLen := state.messageQueue.Len()
	state.queueMu.Unlock()

	// Log large queue growth as it may indicate issues
	if queueLen%1000 == 1 && queueLen > 1000 {
		nsm.logger.Warn("Large message queue for node",
			zap.String("node", string(nodeID)),
			zap.Int("queue_length", queueLen))
	}
}

// SetNodeConnection updates the connection for a node.
func (nsm *NodeStateManager) SetNodeConnection(nodeID cluster.NodeID, conn *NodeConnection, newState ConnectionState) {
	state := nsm.GetOrCreateNodeState(nodeID)

	state.stateMu.Lock()
	defer state.stateMu.Unlock()

	state.connection = conn
	state.state = newState

	nsm.logger.Debug("Updated node connection state",
		zap.String("node", string(nodeID)),
		zap.String("state", newState.String()))
}

// GetNodeConnection returns the current connection and state for a node.
func (nsm *NodeStateManager) GetNodeConnection(nodeID cluster.NodeID) (*NodeConnection, ConnectionState) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return nil, StateNone
	}

	state.stateMu.RLock()
	defer state.stateMu.RUnlock()

	return state.connection, state.state
}

// SetNodeState updates the connection state for a node.
func (nsm *NodeStateManager) SetNodeState(nodeID cluster.NodeID, newState ConnectionState) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return
	}

	state.stateMu.Lock()
	defer state.stateMu.Unlock()

	oldState := state.state
	state.state = newState

	nsm.logger.Debug("Node state transition",
		zap.String("node", string(nodeID)),
		zap.String("from", oldState.String()),
		zap.String("to", newState.String()))
}

// UpdateNodeAddress updates the addressing information for a node.
func (nsm *NodeStateManager) UpdateNodeAddress(nodeID cluster.NodeID, addr string, port int) {
	state := nsm.GetOrCreateNodeState(nodeID)

	state.addr = addr
	state.port = port

	nsm.logger.Debug("Updated node address",
		zap.String("node", string(nodeID)),
		zap.String("addr", fmt.Sprintf("%s:%d", addr, port)))
}

// GetNodeAddress returns the addressing information for a node.
func (nsm *NodeStateManager) GetNodeAddress(nodeID cluster.NodeID) (string, int, bool) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return "", 0, false
	}

	return state.addr, state.port, state.addr != "" && state.port != 0
}

// TryLockConnection attempts to acquire the connection lock for a node.
// Returns true if successful, false if another goroutine is already connecting.
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
		// Invalid message, remove and return nil
		state.messageQueue.Remove(elem)
		nsm.logger.Error("Invalid message type in queue", zap.String("node", string(nodeID)))
		return nil
	}

	state.messageQueue.Remove(elem)
	return data
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

	nsm.logger.Debug("Requeued messages",
		zap.String("node", string(nodeID)),
		zap.Int("count", len(messages)))
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

	state.stateMu.Lock()
	defer state.stateMu.Unlock()

	if state.connection != nil {
		state.connection.Close()
		state.connection = nil
	}

	if state.state != StateDead {
		state.state = StateNone
	}

	nsm.logger.Debug("Closed node connection", zap.String("node", string(nodeID)))
}

// GetConnectedNodes returns a list of currently connected node IDs.
func (nsm *NodeStateManager) GetConnectedNodes() []cluster.NodeID {
	nsm.nodesMu.RLock()
	defer nsm.nodesMu.RUnlock()

	var connected []cluster.NodeID
	for nodeID, state := range nsm.nodeStates {
		state.stateMu.RLock()
		isConnected := state.state == StateConnected
		state.stateMu.RUnlock()

		if isConnected {
			connected = append(connected, nodeID)
		}
	}
	return connected
}

// GetAllNodeStates returns a snapshot of all node states for retry processing.
func (nsm *NodeStateManager) GetAllNodeStates() map[cluster.NodeID]ConnectionState {
	nsm.nodesMu.RLock()
	defer nsm.nodesMu.RUnlock()

	states := make(map[cluster.NodeID]ConnectionState)
	for nodeID, state := range nsm.nodeStates {
		state.stateMu.RLock()
		states[nodeID] = state.state
		state.stateMu.RUnlock()
	}
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
