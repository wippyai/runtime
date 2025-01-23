package manager

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/terminal"
	"go.uber.org/zap"
)

type (
	// TerminalFactory creates new terminal instances.
	TerminalFactory interface {
		MakeTerminal(
			log *zap.Logger,
			cfg api.TerminalConfig,
			modules api.ModuleRegistry,
			libraries api.LibraryRegistry,
		) (terminal.Terminal, error)
	}

	// Terminals handles Lua terminal app operations
	Terminals struct {
		log       *zap.Logger
		dtt       payload.Transcoder
		terminals map[registry.ID]*api.TerminalConfig
		factory   TerminalFactory
	}
)

// NewTerminals creates a new terminal manager instance
func NewTerminals(
	log *zap.Logger,
	dtt payload.Transcoder,
	factory TerminalFactory,
) *Terminals {
	return &Terminals{
		log:       log,
		dtt:       dtt,
		terminals: make(map[registry.ID]*api.TerminalConfig),
		factory:   factory,
	}
}

// Add adds a new terminal configuration
func (m *Terminals) Add(
	entry registry.Entry,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) error {
	cfg := new(api.TerminalConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.terminals[entry.ID]; exists {
		return fmt.Errorf("terminal %s already exists", entry.ID)
	}

	// Validate dependencies
	if err := m.validateDependencies(cfg, modules, libraries); err != nil {
		return err
	}

	m.terminals[entry.ID] = cfg
	m.log.Info("added terminal", zap.String("id", string(entry.ID)))
	return nil
}

// Update updates an existing terminal configuration
func (m *Terminals) Update(
	entry registry.Entry,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) error {
	cfg := new(api.TerminalConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.terminals[entry.ID]; !exists {
		return fmt.Errorf("terminal %s not found", entry.ID)
	}

	// Validate dependencies
	if err := m.validateDependencies(cfg, modules, libraries); err != nil {
		return err
	}

	m.terminals[entry.ID] = cfg
	m.log.Info("updated terminal", zap.String("id", string(entry.ID)))
	return nil
}

// Delete removes a terminal configuration
func (m *Terminals) Delete(entry registry.Entry) error {
	if _, exists := m.terminals[entry.ID]; !exists {
		return fmt.Errorf("terminal %s not found", entry.ID)
	}

	delete(m.terminals, entry.ID)
	m.log.Info("deleted terminal", zap.String("id", string(entry.ID)))
	return nil
}

// GetTerminal retrieves a terminal config by ID
func (m *Terminals) GetTerminal(id registry.ID) (*api.TerminalConfig, bool) {
	term, exists := m.terminals[id]
	return term, exists
}

// FindDependentOnLibrary finds all terminals that depend on a given library
func (m *Terminals) FindDependentOnLibrary(libraryID registry.ID) []registry.ID {
	var dependent []registry.ID
	for id, term := range m.terminals {
		for _, lib := range term.Libraries {
			if registry.ID(lib) == libraryID {
				dependent = append(dependent, id)
				break
			}
		}
	}
	return dependent
}

// MakeTerminal creates a new terminal factory using provided managers
func (m *Terminals) MakeTerminal(
	id registry.ID,
	modules api.ModuleRegistry,
	libraries api.LibraryRegistry,
) (terminal.Terminal, error) {
	term, exists := m.GetTerminal(id)
	if !exists {
		return nil, fmt.Errorf("terminal %s not found", id)
	}

	// Validate dependencies before creating
	if err := m.validateDependencies(term, modules, libraries); err != nil {
		return nil, err
	}

	return m.factory.MakeTerminal(m.log, *term, modules, libraries)
}

// Internal methods

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
		if !libraries.Has(registry.ID(libID)) {
			return fmt.Errorf("library %s not found", libID)
		}
	}

	return nil
}

func (m *Terminals) unmarshalAndValidate(data payload.Payload, cfg interface{}) error {
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
