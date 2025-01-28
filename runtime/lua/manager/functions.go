package manager

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/factory"
	"go.uber.org/zap"
)

// Functions handles Lua function configuration
type Functions struct {
	log       *zap.Logger
	functions map[registry.ID]*api.FunctionConfig
}

// NewFunctions creates a new function manager instance
func NewFunctions(logger *zap.Logger) *Functions {
	return &Functions{
		log:       logger,
		functions: make(map[registry.ID]*api.FunctionConfig),
	}
}

// Add adds a new function with required dependencies
func (m *Functions) Add(
	id registry.ID,
	config *api.FunctionConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) error {
	if err := m.validateDependencies(config, modules, libraries); err != nil {
		return err
	}

	m.functions[id] = config
	m.log.Info("added function", zap.String("id", string(id)))
	return nil
}

// Update updates an existing function with required dependencies
func (m *Functions) Update(
	id registry.ID,
	config *api.FunctionConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) error {
	if _, exists := m.functions[id]; !exists {
		return fmt.Errorf("function %s not found", id)
	}

	if err := m.validateDependencies(config, modules, libraries); err != nil {
		return err
	}

	m.functions[id] = config
	m.log.Info("updated function", zap.String("id", string(id)))
	return nil
}

// Clone creates a new Functions instance with a copy of the functions map
func (m *Functions) Clone() *Functions {
	cloned := &Functions{
		log:       m.log,
		functions: make(map[registry.ID]*api.FunctionConfig, len(m.functions)),
	}

	// Copy map entries (pointers remain the same)
	for id, config := range m.functions {
		cloned.functions[id] = config
	}

	return cloned
}

// Delete removes a function
func (m *Functions) Delete(id registry.ID) error {
	if _, exists := m.functions[id]; !exists {
		return fmt.Errorf("function %s not found", id)
	}

	delete(m.functions, id)
	m.log.Info("deleted function", zap.String("id", string(id)))
	return nil
}

// Get returns a function config by ID
func (m *Functions) Get(id registry.ID) (*api.FunctionConfig, bool) {
	fn, exists := m.functions[id]
	return fn, exists
}

// FindDependentOnLibrary finds all functions that depend on a given library
func (m *Functions) FindDependentOnLibrary(libraryID registry.ID) map[registry.ID]*api.FunctionConfig {
	dependent := make(map[registry.ID]*api.FunctionConfig)
	for id, fn := range m.functions {
		for _, lib := range fn.Libraries {
			if registry.ID(lib) == libraryID {
				dependent[id] = fn
				break
			}
		}
	}
	return dependent
}

// MakeFactory creates a new factory for function compilation using provided managers
func (m *Functions) MakeFactory(
	id registry.ID,
	cfg *api.FunctionConfig,
	logger *zap.Logger,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (api.Factory, error) {
	if err := m.validateDependencies(cfg, modules, libraries); err != nil {
		return nil, err
	}

	fct := factory.NewFactory(logger.Named(fmt.Sprintf("vm.%s", id)))

	knownModules := make(map[string]struct{})

	// Add required modules
	for _, modID := range cfg.Modules {
		module, err := modules.Get(modID)
		if err != nil {
			return nil, err
		}

		if _, exists := knownModules[module.Name()]; exists {
			continue
		}

		fct.Modules = append(fct.Modules, module)
		knownModules[module.Name()] = struct{}{}
	}

	// Add required libraries
	for _, libID := range cfg.Libraries {
		lib, err := libraries.Get(registry.ID(libID))
		if err != nil {
			return nil, err
		}

		fct.Libraries = append(fct.Libraries, factory.Library{
			Name:   libID,
			Script: lib.Source,
		})

		// todo: library also can depend on other libraries
		for _, depID := range lib.Modules {
			dep, err := modules.Get(depID)
			if err != nil {
				return nil, err
			}

			if _, exists := knownModules[dep.Name()]; exists {
				continue
			}

			fct.Modules = append(fct.Modules, dep)
			knownModules[dep.Name()] = struct{}{}
		}
	}

	// Add the function itself
	fct.Functions = append(fct.Functions, factory.Function{
		Name:   cfg.Method,
		Script: cfg.Source,
	})

	return fct, nil
}

func (m *Functions) validateDependencies(
	cfg *api.FunctionConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) error {
	// Validate modules
	for _, modID := range cfg.Modules {
		if !modules.Has(modID) {
			return fmt.Errorf("module %s not found", modID)
		}
	}

	// Validate libraries
	for _, libID := range cfg.Libraries {
		if !libraries.Has(registry.ID(libID)) {
			return fmt.Errorf("library %s not found", libID)
		}
	}

	return nil
}
