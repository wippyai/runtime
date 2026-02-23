// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"container/list"
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

var (
	// ErrNodeNotManaged is returned when an operation is attempted on a node
	// that has not been explicitly registered as a cluster member.
	ErrNodeNotManaged = errors.New("node is not a managed member of the cluster")
)

type NodeState struct {
	messageQueue  *list.List
	messageNotify chan struct{}
	connection    *NodeConnection
	address       nodeAddress
	state         ConnectionState
	stateMu       sync.RWMutex
	queueMu       sync.Mutex
}

type nodeAddress struct {
	addr string
	port int
}

type NodeStateManager struct {
	nodeStates sync.Map // cluster.NodeID -> *NodeState
	logger     *zap.Logger
	config     ManagerConfig
}

func NewNodeStateManager(config ManagerConfig, logger *zap.Logger) *NodeStateManager {
	return &NodeStateManager{
		logger: logger.Named("state"),
		config: config,
	}
}

// CreateNodeState initializes the in-memory state for a new node.
// This should only be called by the manager when a node joins the cluster.
func (nsm *NodeStateManager) CreateNodeState(nodeID cluster.NodeID) {
	if _, ok := nsm.nodeStates.Load(nodeID); ok {
		nsm.logger.Debug("State for node already exists, skipping creation", zap.String("node_id", nodeID))
		return
	}

	nsm.logger.Debug("Creating new managed state for node", zap.String("node_id", nodeID))
	newState := &NodeState{
		messageQueue:  list.New(),
		messageNotify: make(chan struct{}, 1),
		state:         StateNone,
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

// QueueMessage adds a message to a managed node's queue.
// It returns ErrNodeNotManaged if the node state does not exist.
func (nsm *NodeStateManager) QueueMessage(nodeID cluster.NodeID, data []byte) error {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return ErrNodeNotManaged
	}

	if data == nil {
		return nil // Do not queue nil data
	}

	state.queueMu.Lock()
	state.messageQueue.PushBack(data)
	state.queueMu.Unlock()

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

func (nsm *NodeStateManager) DrainMessages(nodeID cluster.NodeID, maxCount int) [][]byte {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return nil
	}

	if maxCount <= 0 {
		return nil
	}

	state.queueMu.Lock()
	defer state.queueMu.Unlock()

	queueLen := state.messageQueue.Len()
	if queueLen == 0 {
		return nil
	}

	drainCount := maxCount
	if queueLen < drainCount {
		drainCount = queueLen
	}
	messages := make([][]byte, 0, drainCount)

	for i := 0; i < drainCount; i++ {
		elem := state.messageQueue.Front()
		if elem == nil {
			break
		}
		if data, ok := elem.Value.([]byte); ok && data != nil {
			messages = append(messages, data)
		}
		state.messageQueue.Remove(elem)
	}

	return messages
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

func (nsm *NodeStateManager) RequeueMessages(nodeID cluster.NodeID, messages [][]byte) {
	if len(messages) == 0 {
		return
	}

	state := nsm.GetNodeState(nodeID)
	if state == nil {
		nsm.logger.Warn("Dropping messages to requeue for unmanaged node",
			zap.String("node_id", nodeID),
			zap.Int("message_count", len(messages)))
		return
	}

	state.queueMu.Lock()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil {
			state.messageQueue.PushFront(messages[i])
		}
	}
	state.queueMu.Unlock()

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
	queueLen := nodeState.messageQueue.Len()
	nodeState.messageQueue.Init()
	nodeState.queueMu.Unlock()

	if queueLen > 0 {
		nsm.logger.Warn("Discarded pending messages for removed node",
			zap.String("node", nodeID),
			zap.Int("discarded_messages", queueLen))
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
