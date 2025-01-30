package workflow

import (
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/service/temporal"
	"go.uber.org/zap"
)

// Manager handles creation and registration of workflow handlers
type Manager struct {
	mu       sync.RWMutex
	log      *zap.Logger
	configs  map[registry.ID]*api.WorkflowDefinition
	workflow runtime.WorkflowRegistry
}

// NewWorkflowManager creates a new workflow manager instance
func NewWorkflowManager(log *zap.Logger, reg runtime.WorkflowRegistry) *Manager {
	return &Manager{
		log:      log,
		configs:  make(map[registry.ID]*api.WorkflowDefinition),
		workflow: reg,
	}
}

// GetHandler retrieves a workflow handler for the given ID
func (m *Manager) GetHandler(id registry.ID) (interface{}, error) {
	m.mu.RLock()
	cfg, exists := m.configs[id]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("workflow configuration %s not found", id)
	}

	// Always get fresh handler from registry
	handler, err := m.workflow.Get(cfg.Workflow)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow handler %s: %w", id, err)
	}

	return handler, nil
}

// InitWorkflow initializes a new workflow configuration
func (m *Manager) InitWorkflow(
	id registry.ID,
	cfg *api.WorkflowDefinition,
) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; exists {
		return nil, fmt.Errorf("workflow configuration %s already exists", id)
	}

	m.configs[id] = cfg
	m.log.Info("initialized workflow configuration", zap.String("id", string(id)))

	return m.workflow.Get(cfg.Workflow)
}

// GetConfig retrieves a workflow configuration
func (m *Manager) GetConfig(id registry.ID) (*api.WorkflowDefinition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg, exists := m.configs[id]
	return cfg, exists
}

// Delete removes a workflow configuration
func (m *Manager) Delete(id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("workflow configuration %s not found", id)
	}

	delete(m.configs, id)
	m.log.Info("deleted workflow configuration", zap.String("id", string(id)))
	return nil
}

// Has checks if a workflow configuration exists
func (m *Manager) Has(id registry.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.configs[id]
	return exists
}
