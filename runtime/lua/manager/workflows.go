package manager

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/workflow"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"go.uber.org/zap"
)

// Workflows handles Lua workflow operations
type Workflows struct {
	log       *zap.Logger
	factory   *workflow.Factory
	workflows map[registry.ID]*api.WorkflowConfig
}

// NewWorkflows creates a new workflow manager instance
func NewWorkflows(logger *zap.Logger, factory *workflow.Factory) *Workflows {
	return &Workflows{
		log:       logger,
		factory:   factory,
		workflows: make(map[registry.ID]*api.WorkflowConfig),
	}
}

// Add adds a new workflow with required dependencies
func (m *Workflows) Add(
	id registry.ID,
	config *api.WorkflowConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) error {
	if err := m.validateDependencies(config, modules, libraries); err != nil {
		return err
	}

	m.workflows[id] = config
	m.log.Info("added workflow", zap.String("id", string(id)))
	return nil
}

// Update updates an existing workflow with required dependencies
func (m *Workflows) Update(
	id registry.ID,
	config *api.WorkflowConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) error {
	if _, exists := m.workflows[id]; !exists {
		return fmt.Errorf("workflow %s not found", id)
	}

	if err := m.validateDependencies(config, modules, libraries); err != nil {
		return err
	}

	m.workflows[id] = config
	m.log.Info("updated workflow", zap.String("id", string(id)))
	return nil
}

// Delete removes a workflow
func (m *Workflows) Delete(id registry.ID) error {
	if _, exists := m.workflows[id]; !exists {
		return fmt.Errorf("workflow %s not found", id)
	}

	delete(m.workflows, id)
	m.log.Info("deleted workflow", zap.String("id", string(id)))
	return nil
}

// Get returns a workflow config by ID
func (m *Workflows) Get(id registry.ID) (*api.WorkflowConfig, bool) {
	wf, exists := m.workflows[id]
	return wf, exists
}

// FindDependentOnLibrary finds all workflows that depend on a given library
func (m *Workflows) FindDependentOnLibrary(libraryID registry.ID) map[registry.ID]*api.WorkflowConfig {
	dependent := make(map[registry.ID]*api.WorkflowConfig)
	for id, wf := range m.workflows {
		for _, lib := range wf.Libraries {
			if registry.ID(lib) == libraryID {
				dependent[id] = wf
				break
			}
		}
	}
	return dependent
}

// GetFactory creates a new factory for workflow compilation using provided managers
func (m *Workflows) GetFactory(
	id registry.ID,
	cfg *api.WorkflowConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (func() *workflow.Runner, error) {
	if err := m.validateDependencies(cfg, modules, libraries); err != nil {
		return nil, err
	}

	return m.factory.ForWorkflow(m.log, cfg, modules, libraries)
}

// validateDependencies validates workflow configuration dependencies
func (m *Workflows) validateDependencies(
	cfg *api.WorkflowConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) error {
	// Validate libraries
	for _, libID := range cfg.Libraries {
		if !libraries.Has(registry.ID(libID)) {
			return fmt.Errorf("library %s not found", libID)
		}
	}

	return nil
}
