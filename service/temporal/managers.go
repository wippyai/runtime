package temporal

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// ClientManager handles Temporal client configurations and lifecycle
type ClientManager struct {
	log     *zap.Logger
	clients map[registry.ID]*Client
}

// NewClientManager creates a new client manager instance
func NewClientManager(logger *zap.Logger) *ClientManager {
	return &ClientManager{
		log:     logger,
		clients: make(map[registry.ID]*Client),
	}
}

func (m *ClientManager) Add(id registry.ID, config *ClientConfig) error {
	if _, exists := m.clients[id]; exists {
		return fmt.Errorf("client %s already exists", id)
	}

	m.clients[id] = NewClient(*config)
	m.log.Info("added client", zap.String("id", string(id)))
	return nil
}

func (m *ClientManager) Delete(id registry.ID) error {
	if _, exists := m.clients[id]; !exists {
		return fmt.Errorf("client %s not found", id)
	}

	delete(m.clients, id)
	m.log.Info("deleted client", zap.String("id", string(id)))
	return nil
}

func (m *ClientManager) Get(id registry.ID) (*Client, bool) {
	client, exists := m.clients[id]
	return client, exists
}

// TaskQueueManager handles Temporal task queue configurations and lifecycle
type TaskQueueManager struct {
	log     *zap.Logger
	queues  map[registry.ID]*TaskQueue
	clients *ClientManager
}

func NewTaskQueueManager(logger *zap.Logger, clients *ClientManager) *TaskQueueManager {
	return &TaskQueueManager{
		log:     logger,
		queues:  make(map[registry.ID]*TaskQueue),
		clients: clients,
	}
}

func (m *TaskQueueManager) Add(id registry.ID, config *TaskQueueConfig) error {
	clientID := registry.ID(config.Meta.StringValue(ClientKey))
	client, exists := m.clients.Get(clientID)
	if !exists {
		return fmt.Errorf("client %s not found", clientID)
	}

	if _, exists := m.queues[id]; exists {
		return fmt.Errorf("task queue %s already exists", id)
	}

	m.queues[id] = NewTaskQueue(*config, client)
	m.log.Info("added task queue", zap.String("id", string(id)))
	return nil
}

func (m *TaskQueueManager) Update(id registry.ID, config *TaskQueueConfig) error {
	queue, exists := m.queues[id]
	if !exists {
		return fmt.Errorf("task queue %s not found", id)
	}

	queue.UpdateConfig(*config)
	m.log.Info("updated task queue", zap.String("id", string(id)))
	return nil
}

func (m *TaskQueueManager) Delete(id registry.ID) error {
	if _, exists := m.queues[id]; !exists {
		return fmt.Errorf("task queue %s not found", id)
	}

	delete(m.queues, id)
	m.log.Info("deleted task queue", zap.String("id", string(id)))
	return nil
}

func (m *TaskQueueManager) Get(id registry.ID) (*TaskQueue, bool) {
	queue, exists := m.queues[id]
	return queue, exists
}

// WorkflowManager handles workflow definitions
type WorkflowManager struct {
	log      *zap.Logger
	wflows   map[registry.ID]*WorkflowDef
	queues   *TaskQueueManager
	queueMap map[registry.ID]registry.ID // workflow ID -> queue ID
}

func NewWorkflowManager(logger *zap.Logger, queues *TaskQueueManager) *WorkflowManager {
	return &WorkflowManager{
		log:      logger,
		wflows:   make(map[registry.ID]*WorkflowDef),
		queues:   queues,
		queueMap: make(map[registry.ID]registry.ID),
	}
}

func (m *WorkflowManager) Add(id registry.ID, config *WorkflowConfig) error {
	queueID := registry.ID(config.Meta.StringValue(TaskQueueKey))
	queue, exists := m.queues.Get(queueID)
	if !exists {
		return fmt.Errorf("task queue %s not found", queueID)
	}

	wf := NewWorkflowDef(*config)
	m.wflows[id] = wf
	m.queueMap[id] = queueID

	queue.RegisterWorkflow(id, wf)
	m.log.Info("added workflow", zap.String("id", string(id)))
	return nil
}

func (m *WorkflowManager) Update(id registry.ID, config *WorkflowConfig) error {
	queueID := registry.ID(config.Meta.StringValue(TaskQueueKey))
	queue, exists := m.queues.Get(queueID)
	if !exists {
		return fmt.Errorf("task queue %s not found", queueID)
	}

	wf := NewWorkflowDef(*config)
	currentQueueID := m.queueMap[id]

	// Handle queue migration if needed
	if currentQueueID != queueID {
		if oldQueue, exists := m.queues.Get(currentQueueID); exists {
			oldQueue.UnregisterWorkflow(id)
		}
	}

	m.wflows[id] = wf
	m.queueMap[id] = queueID
	queue.RegisterWorkflow(id, wf)

	m.log.Info("updated workflow", zap.String("id", string(id)))
	return nil
}

func (m *WorkflowManager) Delete(id registry.ID) error {
	queueID, exists := m.queueMap[id]
	if !exists {
		return fmt.Errorf("workflow %s not found", id)
	}

	if queue, exists := m.queues.Get(queueID); exists {
		queue.UnregisterWorkflow(id)
	}

	delete(m.wflows, id)
	delete(m.queueMap, id)
	m.log.Info("deleted workflow", zap.String("id", string(id)))
	return nil
}

// ActivityManager handles activity definitions
type ActivityManager struct {
	log      *zap.Logger
	acts     map[registry.ID]*ActivityDef
	queues   *TaskQueueManager
	queueMap map[registry.ID]registry.ID // activity ID -> queue ID
}

func NewActivityManager(logger *zap.Logger, queues *TaskQueueManager) *ActivityManager {
	return &ActivityManager{
		log:      logger,
		acts:     make(map[registry.ID]*ActivityDef),
		queues:   queues,
		queueMap: make(map[registry.ID]registry.ID),
	}
}

func (m *ActivityManager) Add(id registry.ID, config *ActivityConfig) error {
	queueID := registry.ID(config.Meta.StringValue(TaskQueueKey))
	queue, exists := m.queues.Get(queueID)
	if !exists {
		return fmt.Errorf("task queue %s not found", queueID)
	}

	act := NewActivityDef(*config)
	m.acts[id] = act
	m.queueMap[id] = queueID

	queue.RegisterActivity(id, act)
	m.log.Info("added activity", zap.String("id", string(id)))
	return nil
}

func (m *ActivityManager) Update(id registry.ID, config *ActivityConfig) error {
	queueID := registry.ID(config.Meta.StringValue(TaskQueueKey))
	queue, exists := m.queues.Get(queueID)
	if !exists {
		return fmt.Errorf("task queue %s not found", queueID)
	}

	act := NewActivityDef(*config)
	currentQueueID := m.queueMap[id]

	// Handle queue migration if needed
	if currentQueueID != queueID {
		if oldQueue, exists := m.queues.Get(currentQueueID); exists {
			oldQueue.UnregisterActivity(id)
		}
	}

	m.acts[id] = act
	m.queueMap[id] = queueID
	queue.RegisterActivity(id, act)

	m.log.Info("updated activity", zap.String("id", string(id)))
	return nil
}

func (m *ActivityManager) Delete(id registry.ID) error {
	queueID, exists := m.queueMap[id]
	if !exists {
		return fmt.Errorf("activity %s not found", id)
	}

	if queue, exists := m.queues.Get(queueID); exists {
		queue.UnregisterActivity(id)
	}

	delete(m.acts, id)
	delete(m.queueMap, id)
	m.log.Info("deleted activity", zap.String("id", string(id)))
	return nil
}

// FindDependentOnQueue finds all activities attached to a queue
func (m *ActivityManager) FindDependentOnQueue(queueID registry.ID) []registry.ID {
	var dependent []registry.ID
	for id, mapped := range m.queueMap {
		if mapped == queueID {
			dependent = append(dependent, id)
		}
	}
	return dependent
}
