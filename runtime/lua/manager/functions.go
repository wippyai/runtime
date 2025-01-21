package manager

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/factory"
	"go.uber.org/zap"
)

// Functions handles Lua function configuration
type Functions struct {
	log       *zap.Logger
	dtt       payload.Transcoder
	functions map[registry.ID]*api.FunctionConfig
}

// NewFunctions creates a new function manager instance
func NewFunctions(dtt payload.Transcoder, logger *zap.Logger) *Functions {
	return &Functions{
		log:       logger,
		dtt:       dtt,
		functions: make(map[registry.ID]*api.FunctionConfig),
	}
}

// Add adds a new function with required dependencies
func (m *Functions) Add(
	entry registry.Entry,
	modules *Modules,
	libraries *Libraries,
) error {
	cfg := new(api.FunctionConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.functions[entry.ID]; exists {
		return fmt.Errorf("function %s already exists", entry.ID)
	}

	if err := m.validateDependencies(cfg, modules, libraries); err != nil {
		return err
	}

	m.functions[entry.ID] = cfg
	m.log.Info("added function", zap.String("id", string(entry.ID)))
	return nil
}

// Update updates an existing function with required dependencies
func (m *Functions) Update(
	entry registry.Entry,
	modules *Modules,
	libraries *Libraries,
) error {
	cfg := new(api.FunctionConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.functions[entry.ID]; !exists {
		return fmt.Errorf("function %s not found", entry.ID)
	}

	if err := m.validateDependencies(cfg, modules, libraries); err != nil {
		return err
	}

	m.functions[entry.ID] = cfg
	m.log.Info("updated function", zap.String("id", string(entry.ID)))
	return nil
}

// Delete removes a function
func (m *Functions) Delete(entry registry.Entry) error {
	if _, exists := m.functions[entry.ID]; !exists {
		return fmt.Errorf("function %s not found", entry.ID)
	}

	delete(m.functions, entry.ID)
	m.log.Info("deleted function", zap.String("id", string(entry.ID)))
	return nil
}

// GetFunction returns a function config by ID
func (m *Functions) GetFunction(id registry.ID) (*api.FunctionConfig, bool) {
	fn, exists := m.functions[id]
	return fn, exists
}

// FindDependentOnLibrary finds all functions that depend on a given library
func (m *Functions) FindDependentOnLibrary(libraryID registry.ID) []registry.ID {
	var dependent []registry.ID
	for id, fn := range m.functions {
		for _, lib := range fn.Libraries {
			if registry.ID(lib) == libraryID {
				dependent = append(dependent, id)
				break
			}
		}
	}
	return dependent
}

// MakeFactory creates a new factory for function compilation using provided managers
func (m *Functions) MakeFactory(
	id registry.ID,
	modules *Modules,
	libraries *Libraries,
	logger *zap.Logger,
) (api.Factory, error) {
	fn, exists := m.GetFunction(id)
	if !exists {
		return nil, fmt.Errorf("function %s not found", id)
	}

	cfg := factory.NewFactory(logger.Named(fmt.Sprintf("vm.%s", id)))

	// Add required modules
	for _, modID := range fn.Modules {
		module, exists := modules.Get(modID)
		if !exists {
			return nil, fmt.Errorf("module %s not found", modID)
		}
		cfg.Modules = append(cfg.Modules, module)
	}

	// Add required libraries
	for _, libID := range fn.Libraries {
		lib, exists := libraries.GetLibrary(registry.ID(libID))
		if !exists {
			return nil, fmt.Errorf("library %s not found", libID)
		}
		cfg.Libraries = append(cfg.Libraries, factory.Library{
			Name:   libID,
			Script: lib.Source,
		})
	}

	// Add the function itself
	cfg.Functions = append(cfg.Functions, factory.Function{
		Name:   fn.Method,
		Script: fn.Source,
	})

	return cfg, nil
}

// Internal methods

func (m *Functions) validateDependencies(
	cfg *api.FunctionConfig,
	modules *Modules,
	libraries *Libraries,
) error {
	// Validate modules
	for _, modID := range cfg.Modules {
		if _, exists := modules.Get(modID); !exists {
			return fmt.Errorf("module %s not found", modID)
		}
	}

	// Validate libraries
	for _, libID := range cfg.Libraries {
		if !libraries.HasLibrary(registry.ID(libID)) {
			return fmt.Errorf("library %s not found", libID)
		}
	}

	return nil
}

func (m *Functions) unmarshalAndValidate(data payload.Payload, cfg interface{}) error {
	if err := m.dtt.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if validator, ok := cfg.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return nil
}
