package manager

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/shell"
	"go.uber.org/zap"
)

// TerminalFactory creates new terminal instances.
type TerminalFactory interface {
	MakeTerminal(
		log *zap.Logger,
		app *api.TerminalConfig,
		modules api.ModuleRegistry,
		libraries api.LibraryRegistry,
	) (shell.Terminal, error)
}

// Terminals handles Lua terminal app operations
type Terminals struct {
	log       *zap.Logger
	terminals map[registry.Name]*api.TerminalConfig
	factory   TerminalFactory
}

// NewTerminals creates a new terminal manager instance
func NewTerminals(
	log *zap.Logger,
	factory TerminalFactory,
) *Terminals {
	return &Terminals{
		log:       log,
		terminals: make(map[registry.Name]*api.TerminalConfig),
		factory:   factory,
	}
}

// Add adds a new terminal configuration
func (m *Terminals) Add(
	id registry.Name,
	config *api.TerminalConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) error {
	if _, exists := m.terminals[id]; exists {
		return fmt.Errorf("terminal %s already exists", id)
	}

	// Validate dependencies
	if err := m.validateDependencies(config, modules, libraries); err != nil {
		return err
	}

	m.terminals[id] = config
	m.log.Info("added terminal", zap.String("id", string(id)))
	return nil
}

// Update updates an existing terminal configuration
func (m *Terminals) Update(
	id registry.Name,
	config *api.TerminalConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) error {
	if _, exists := m.terminals[id]; !exists {
		return fmt.Errorf("terminal %s not found", id)
	}

	// Validate dependencies
	if err := m.validateDependencies(config, modules, libraries); err != nil {
		return err
	}

	m.terminals[id] = config
	m.log.Info("updated terminal", zap.String("id", string(id)))
	return nil
}

// Delete removes a terminal configuration
func (m *Terminals) Delete(id registry.Name) error {
	if _, exists := m.terminals[id]; !exists {
		return fmt.Errorf("terminal %s not found", id)
	}

	delete(m.terminals, id)
	m.log.Info("deleted terminal", zap.String("id", string(id)))
	return nil
}

// GetTerminal retrieves a terminal config by Name
func (m *Terminals) GetTerminal(id registry.Name) (*api.TerminalConfig, bool) {
	term, exists := m.terminals[id]
	return term, exists
}

// FindDependentOnLibrary finds all terminals that depend on a given library
func (m *Terminals) FindDependentOnLibrary(libraryID registry.Name) map[registry.Name]*api.TerminalConfig {
	var dependent = make(map[registry.Name]*api.TerminalConfig)
	for id, term := range m.terminals {
		for _, lib := range term.Libraries {
			if registry.Name(lib) == libraryID {
				dependent[id] = term
				break
			}
		}
	}
	return dependent
}

// MakeTerminal creates a new terminal factory using provided managers
func (m *Terminals) MakeTerminal(
	_ registry.Name,
	app *api.TerminalConfig,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (shell.Terminal, error) {
	if err := m.validateDependencies(app, modules, libraries); err != nil {
		return nil, err
	}

	return m.factory.MakeTerminal(m.log, app, modules, libraries)
}

// validateDependencies validates terminal configuration dependencies
func (m *Terminals) validateDependencies(
	cfg *api.TerminalConfig,
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
		if !libraries.Has(registry.Name(libID)) {
			return fmt.Errorf("library %s not found", libID)
		}
	}

	return nil
}
