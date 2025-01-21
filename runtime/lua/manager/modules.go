package manager

import (
	"fmt"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"go.uber.org/zap"
)

// Modules handles Lua module registration and access
type Modules struct {
	log     *zap.Logger
	modules map[string]api.Module
}

// NewModules creates a new module manager instance
func NewModules(logger *zap.Logger) *Modules {
	return &Modules{
		log:     logger,
		modules: make(map[string]api.Module),
	}
}

// Register adds a new module to the manager
func (m *Modules) Register(module api.Module) error {
	name := module.Name()
	if _, exists := m.modules[name]; exists {
		return fmt.Errorf("module %s already registered", name)
	}

	m.modules[name] = module
	m.log.Debug("registered module", zap.String("name", name))
	return nil
}

// Unregister removes a module from the manager
func (m *Modules) Unregister(name string) error {
	if _, exists := m.modules[name]; !exists {
		return fmt.Errorf("module %s not found", name)
	}

	delete(m.modules, name)
	m.log.Debug("unregistered module", zap.String("name", name))
	return nil
}

// Get returns a module by name
func (m *Modules) Get(name string) (api.Module, error) {
	module, exists := m.modules[name]
	if !exists {
		return nil, fmt.Errorf("module %s not found", name)
	}

	return module, nil
}

func (m *Modules) Has(name string) bool {
	_, exists := m.modules[name]
	return exists
}

// List returns all registered module names
func (m *Modules) List() []string {
	names := make([]string, 0, len(m.modules))
	for name := range m.modules {
		names = append(names, name)
	}
	return names
}
