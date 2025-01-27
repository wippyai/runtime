package lua

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/terminal"
)

func (m *RuntimeManager) compileTerminal(id registry.ID, cfg *api.TerminalConfig) (terminal.Terminal, error) {
	instance, err := m.terminals.MakeTerminal(id, cfg, m.modules, m.libraries)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	return instance, nil
}

func (m *RuntimeManager) compileFunction(id registry.ID, cfg *api.FunctionConfig) (api.Callable, error) {
	factory, err := m.functions.MakeFactory(id, cfg, m.log, m.modules, m.libraries)
	if err != nil {
		return nil, fmt.Errorf("factory creation failed: %w", err)
	}

	if err := factory.Compile(); err != nil {
		return nil, fmt.Errorf("compilation failed: %w", err)
	}

	handler, err := m.pools.Build(factory, cfg)
	if err != nil {
		return nil, fmt.Errorf("pool creation failed: %w", err)
	}

	return handler, nil
}

// validateLibraryUpdateDependencies checks if all dependent functions and terminals
// can still be compiled after a library update
func (m *RuntimeManager) validateLibraryUpdateDependencies(
	libraryID registry.ID,
	newLibConfig *api.LibraryConfig,
) (map[registry.ID]*api.FunctionConfig, map[registry.ID]*api.TerminalConfig, error) {
	// Temporarily apply the new library config to test compilation
	if !m.libraries.Has(libraryID) {
		return nil, nil, fmt.Errorf("library %s not found", libraryID)
	}

	// Create a temporary copy of the library manager with the new config
	tempLibraries := m.libraries.Clone()
	if err := tempLibraries.Update(libraryID, newLibConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to update library: %w", err)
	}

	// Check dependent functions
	dependentFuncs := m.functions.FindDependentOnLibrary(libraryID)
	for id, fnCfg := range dependentFuncs {
		factory, err := m.functions.MakeFactory(
			id,
			fnCfg,
			m.log,
			m.modules,
			tempLibraries,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("dependent function compilation check failed: %w", err)
		}

		// Try to compile the function
		if err := factory.Compile(); err != nil {
			return nil, nil, fmt.Errorf("library update would break dependent function compilation: %w", err)
		}
	}

	// Check dependent terminals
	dependentTerms := m.terminals.FindDependentOnLibrary(libraryID)
	for id, termCfg := range dependentTerms {
		// Try to create a terminal instance with the new library config
		_, err := m.terminals.MakeTerminal(
			id,
			termCfg,
			m.modules,
			tempLibraries,
		)

		// we can't really verify terminal safely yet
		// todo: use-preemplive testing in future with cloned registry

		if err != nil {
			return nil, nil, fmt.Errorf("library update would break dependent terminal compilation: %w", err)
		}
	}

	return dependentFuncs, dependentTerms, nil
}
