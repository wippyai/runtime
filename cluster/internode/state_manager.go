package internode

import (
	"container/list"
	"sync"

	"github.com/ponyruntime/pony/api/cluster"
	"go.uber.org/zap"
)

type NodeState struct {
	messageQueue  *list.List
	queueMu       sync.Mutex
	messageNotify chan struct{}
	connection    *NodeConnection
	state         ConnectionState
	stateMu       sync.RWMutex
	address       nodeAddress
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

func (nsm *NodeStateManager) GetOrCreateNodeState(nodeID cluster.NodeID) *NodeState {
	if state, ok := nsm.nodeStates.Load(nodeID); ok {
		return state.(*NodeState)
	}

	newState := &NodeState{
		messageQueue:  list.New(),
		messageNotify: make(chan struct{}, 1),
		state:         StateNone,
	}

	if actual, loaded := nsm.nodeStates.LoadOrStore(nodeID, newState); loaded {
		return actual.(*NodeState)
	}

	return newState
}

func (nsm *NodeStateManager) GetNodeState(nodeID cluster.NodeID) *NodeState {
	if state, ok := nsm.nodeStates.Load(nodeID); ok {
		return state.(*NodeState)
	}
	return nil
}

func (nsm *NodeStateManager) QueueMessage(nodeID cluster.NodeID, data []byte) {
	if data == nil {
		return
	}

	state := nsm.GetOrCreateNodeState(nodeID)

	state.queueMu.Lock()
	state.messageQueue.PushBack(data)
	state.queueMu.Unlock()

	select {
	case state.messageNotify <- struct{}{}:
	default:
	}
}

func (nsm *NodeStateManager) SetNodeConnection(nodeID cluster.NodeID, conn *NodeConnection, newState ConnectionState) {
	state := nsm.GetOrCreateNodeState(nodeID)

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
		return
	}

	state.stateMu.Lock()
	state.state = newState
	state.stateMu.Unlock()
}

func (nsm *NodeStateManager) UpdateNodeAddress(nodeID cluster.NodeID, addr string, port int) {
	state := nsm.GetOrCreateNodeState(nodeID)

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
	state.messageQueue.Remove(elem)

	if !ok || data == nil {
		return nil
	}

	return data
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

	// Pre-allocate with exact size needed
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
		return nil
	}
	return state.messageNotify
}

func (nsm *NodeStateManager) GetQueueLength(nodeID cluster.NodeID) int {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return 0
	}

	state.queueMu.Lock()
	length := state.messageQueue.Len()
	state.queueMu.Unlock()

	return length
}

func (nsm *NodeStateManager) RequeueMessages(nodeID cluster.NodeID, messages [][]byte) {
	if len(messages) == 0 {
		return
	}

	state := nsm.GetOrCreateNodeState(nodeID)

	state.queueMu.Lock()
	defer state.queueMu.Unlock()

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil {
			state.messageQueue.PushFront(messages[i])
		}
	}

	select {
	case state.messageNotify <- struct{}{}:
	default:
	}
}

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
	state.state = StateNone
}

func (nsm *NodeStateManager) GetConnectedNodes() []cluster.NodeID {
	var connected []cluster.NodeID

	nsm.nodeStates.Range(func(key, value interface{}) bool {
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

func (nsm *NodeStateManager) GetAllNodeStates() map[cluster.NodeID]ConnectionState {
	states := make(map[cluster.NodeID]ConnectionState)

	nsm.nodeStates.Range(func(key, value interface{}) bool {
		nodeID := key.(cluster.NodeID)
		state := value.(*NodeState)

		state.stateMu.RLock()
		currentState := state.state
		state.stateMu.RUnlock()

		states[nodeID] = currentState
		return true
	})

	return states
}

func (nsm *NodeStateManager) RemoveNode(nodeID cluster.NodeID) {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return
	}

	nsm.logger.Info("Removing node from memory", zap.String("node", string(nodeID)))

	state.stateMu.Lock()
	if state.connection != nil {
		state.connection.Close()
		state.connection = nil
	}
	state.state = StateNone
	state.stateMu.Unlock()

	state.queueMu.Lock()
	queueLen := state.messageQueue.Len()
	state.messageQueue.Init()
	state.queueMu.Unlock()

	nsm.nodeStates.Delete(nodeID)

	if queueLen > 0 {
		nsm.logger.Info("Removed node with pending messages",
			zap.String("node", string(nodeID)),
			zap.Int("discarded_messages", queueLen))
	}
}

func (nsm *NodeStateManager) GetNodeStateSummary(nodeID cluster.NodeID) map[string]any {
	state := nsm.GetNodeState(nodeID)
	if state == nil {
		return map[string]any{"exists": false}
	}

	state.stateMu.RLock()
	currentState := state.state
	hasConnection := state.connection != nil
	addr := state.address
	state.stateMu.RUnlock()

	state.queueMu.Lock()
	queueLen := state.messageQueue.Len()
	state.queueMu.Unlock()

	return map[string]any{
		"exists":         true,
		"state":          currentState.String(),
		"has_connection": hasConnection,
		"queue_length":   queueLen,
		"address":        addr.addr,
		"port":           addr.port,
	}
}
