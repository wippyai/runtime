package task_queue

import (
	"fmt"
	api "github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/service/temporal/client"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// Manager handles Temporal task queue configuration and lifecycle
type Manager struct {
	mu       sync.RWMutex
	log      *zap.Logger
	configs  map[registry.ID]*api.TaskQueueConfig
	services map[registry.ID]*TaskQueue
}

// NewTaskQueueManager creates a new task queue manager instance
func NewTaskQueueManager(logger *zap.Logger) *Manager {
	return &Manager{
		log:      logger,
		configs:  make(map[registry.ID]*api.TaskQueueConfig),
		services: make(map[registry.ID]*TaskQueue),
	}
}

// Add adds a new task queue configuration
func (m *Manager) Add(id registry.ID, config *api.TaskQueueConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; exists {
		return fmt.Errorf("task queue %s already exists", id)
	}

	m.configs[id] = config
	m.log.Info("added task queue config",
		zap.String("id", string(id)),
		zap.String("task_queue", config.TaskQueue),
	)
	return nil
}

// Update updates an existing task queue configuration
func (m *Manager) Update(id registry.ID, config *api.TaskQueueConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("task queue %s not found", id)
	}

	m.configs[id] = config
	m.log.Info("updated task queue config",
		zap.String("id", string(id)),
		zap.String("task_queue", config.TaskQueue),
	)

	// todo: we probably want to propagate this change to the service

	return nil
}

// Delete removes a task queue configuration and service if it exists
func (m *Manager) Delete(id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("task queue %s not found", id)
	}

	delete(m.configs, id)
	delete(m.services, id) // Service cleanup should be handled by supervisor
	m.log.Info("deleted task queue", zap.String("id", string(id)))
	return nil
}

// GetConfig retrieves a task queue config by ID
func (m *Manager) GetConfig(id registry.ID) (*api.TaskQueueConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	config, exists := m.configs[id]
	return config, exists
}

// GetTaskQueue returns an existing service or creates a new one
func (m *Manager) GetTaskQueue(id registry.ID, client *client.Client) (*TaskQueue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for existing service
	if service, exists := m.services[id]; exists {
		return service, nil
	}

	// Get config
	config, exists := m.configs[id]
	if !exists {
		return nil, fmt.Errorf("task queue %s not found", id)
	}

	// Create new service
	service := NewTaskQueue(m.log, config, client)
	m.services[id] = service

	return service, nil
}

// GetActiveTaskQueues returns the number of task queues configured for a specific client
func (m *Manager) GetActiveTaskQueues(clientID registry.ID) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, cfg := range m.configs {
		if cfg.Client == clientID && cfg.Lifecycle.AutoStart {
			count++
		}
	}

	return count
}

// Has checks if a task queue exists
func (m *Manager) Has(id registry.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.configs[id]
	return exists
}
