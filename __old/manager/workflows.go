package manager

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/process"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"go.uber.org/zap"
)

// Workflows handles Lua workflow operations
type Workflows struct {
	log       *zap.Logger
	factory   *process.Factory
	workflows map[registry.Name]*api.WorkflowConfig
}

// NewWorkflows creates a new workflow manager instance
func NewWorkflows(logger *zap.Logger, factory *process.Factory) *Workflows {
	return &Workflows{
		log:       logger,
		factory:   factory,
		workflows: make(map[registry.Name]*api.WorkflowConfig),
	}
}

// Add adds a new workflow with required dependencies
func (m *Workflows) Add(
	id registry.Name,
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
	id registry.Name,
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
func (m *Workflows) Delete(id registry.Name) error {
	if _, exists := m.workflows[id]; !exists {
		return fmt.Errorf("workflow %s not found", id)
	}

	delete(m.workflows, id)
	m.log.Info("deleted workflow", zap.String("id", string(id)))
	return nil
}

// Get returns a workflow config by Name
func (m *Workflows) Get(id registry.Name) (*api.WorkflowConfig, bool) {
	wf, exists := m.workflows[id]
	return wf, exists
}

// FindDependentOnLibrary finds all workflows that depend on a given library
func (m *Workflows) FindDependentOnLibrary(libraryID registry.Name) map[registry.Name]*api.WorkflowConfig {
	dependent := make(map[registry.Name]*api.WorkflowConfig)
	for id, wf := range m.workflows {
		for _, lib := range wf.Libraries {
			if registry.Name(lib) == libraryID {
				dependent[id] = wf
				break
			}
		}
	}
	return dependent
}

// GetFactory creates a new factory for workflow compilation using provided managers
func (m *Workflows) GetFactory(
	id registry.Name,
	cfg *api.WorkflowConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (func() any, error) {
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
		if !libraries.Has(registry.Name(libID)) {
			return fmt.Errorf("library %s not found", libID)
		}
	}

	return nil
}
