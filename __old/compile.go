package __old

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/manager"
	lua "github.com/yuin/gopher-lua"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/shell"
)

func (m *RuntimeManager) compileTerminal(id registry.Name, cfg *api.TerminalConfig) (shell.Terminal, error) {
	instance, err := m.terminals.MakeTerminal(id, cfg, m.modules, m.libraries)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	return instance, nil
}

func (m *RuntimeManager) compileFunction(id registry.Name, cfg *api.FunctionConfig) (api.Callable, error) {
	factory, err := m.function.MakeFactory(id, cfg, m.log, m.modules, m.libraries)
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

func (m *RuntimeManager) compileWorkflow(id registry.Name, cfg *api.WorkflowConfig) (func() any, error) {
	runner, err := m.workflows.GetFactory(id, cfg, m.modules, m.libraries)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow: %w", err)
	}

	return runner, nil
}

func (m *RuntimeManager) validateWorkflowDependencies(
	libraryID registry.Name,
	newLibConfig *api.LibraryConfig,
	tempLibraries *manager.Libraries,
) (map[registry.Name]*api.WorkflowConfig, error) {
	// Find dependent workflows
	dependentWorkflows := m.workflows.FindDependentOnLibrary(libraryID)
	//for id, wfCfg := range dependentWorkflows {
	// Try to create a workflow with new library config
	//f, err := m.workflows.GetFactory(
	//	id,
	//	wfCfg,
	//	m.modules,
	//	tempLibraries,
	//)
	//if err != nil {
	//	return nil, fmt.Errorf("library update would break dependent workflow compilation: %w", err)
	//}

	//f()
	//}

	// todo: implement properly
	return dependentWorkflows, nil
}

// validateLibraryUpdateDependencies checks if all dependent functions and terminals
// can still be compiled after a library update
func (m *RuntimeManager) validateLibraryUpdateDependencies(
	libraryID registry.Name,
	newLibConfig *api.LibraryConfig,
) (map[registry.Name]*api.FunctionConfig, map[registry.Name]*api.TerminalConfig, map[registry.Name]*api.WorkflowConfig, error) {
	// Temporarily apply the new library config to test compilation
	if !m.libraries.Has(libraryID) {
		return nil, nil, nil, fmt.Errorf("library %s not found", libraryID)
	}

	// Create a temporary copy of the library manager with the new config
	tempLibraries := m.libraries.Clone()
	if err := tempLibraries.Update(libraryID, newLibConfig); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to update library: %w", err)
	}

	// Check dependent functions
	dependentFuncs := m.function.FindDependentOnLibrary(libraryID)
	for id, fnCfg := range dependentFuncs {
		factory, err := m.function.MakeFactory(
			id,
			fnCfg,
			m.log,
			m.modules,
			tempLibraries,
		)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("dependent function compilation check failed: %w", err)
		}

		if err := factory.Compile(); err != nil {
			return nil, nil, nil, fmt.Errorf("library update would break dependent function compilation: %w", err)
		}
	}

	// Check dependent workflows
	dependentWorkflows, err := m.validateWorkflowDependencies(libraryID, newLibConfig, tempLibraries)
	if err != nil {
		return nil, nil, nil, err
	}

	// Check dependent terminals
	dependentTerms := m.terminals.FindDependentOnLibrary(libraryID)
	for id, termCfg := range dependentTerms {
		_, err := m.terminals.MakeTerminal(
			id,
			termCfg,
			m.modules,
			tempLibraries,
		)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("library update would break dependent terminal compilation: %w", err)
		}
	}

	return dependentFuncs, dependentTerms, dependentWorkflows, nil
}
