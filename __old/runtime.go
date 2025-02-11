package __old

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/runtime/lua/process"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/shell"
	"github.com/ponyruntime/pony/runtime/lua/manager"
	"github.com/ponyruntime/pony/runtime/lua/pool"
	terminalmng "github.com/ponyruntime/pony/runtime/lua/terminal"
	"go.uber.org/zap"
)

// we can always move dep managedmenet into graph

// RuntimeManager handles Lua runtime operations using separate managers
type RuntimeManager struct {
	log       *zap.Logger
	bus       events.Bus
	dtt       payload.Transcoder
	functions *manager.Functions
	libraries *manager.Libraries
	terminals *manager.Terminals
	workflows *manager.Workflows
	modules   *manager.Modules
	pools     *pool.Factory
	callable  sync.Map
}

// NewRuntimeManager creates a new Lua runtime manager instance
func NewRuntimeManager(
	bus events.Bus,
	dtt payload.Transcoder,
	logger *zap.Logger,
	modules ...api.Module,
) *RuntimeManager {
	m := &RuntimeManager{
		log:       logger,
		bus:       bus,
		dtt:       dtt,
		functions: manager.NewFunctions(logger),
		libraries: manager.NewLibraries(logger),
		terminals: manager.NewTerminals(logger, terminalmng.NewFactory()),
		workflows: manager.NewWorkflows(logger, process.NewFactory()),
		modules:   manager.NewModules(logger),
		callable:  sync.Map{},
	}

	// Register initial modules
	for _, module := range modules {
		if err := m.modules.Register(module); err != nil {
			logger.Error("failed to register module", zap.String("name", module.Name()), zap.Error(err))
		}
	}

	m.pools = pool.NewFactory(logger.Named("pool"))
	return m
}

// Add implements registry.EntryListener
func (m *RuntimeManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required for create operation")
	}

	switch entry.Kind {
	case api.KindFunction:
		cfg, err := m.unpackFunction(entry.Data)
		if err != nil {
			return err
		}

		handler, err := m.compileFunction(entry.ID, cfg)
		if err != nil {
			return err
		}

		if err := m.functions.Add(entry.ID, cfg, m.modules, m.libraries); err != nil {
			return err
		}

		m.callable.Store(entry.ID, handler)
		m.registerHandler(ctx, entry.ID)
		return nil

	case api.KindLibrary:
		cfg, err := m.unpackLibrary(entry.Data)
		if err != nil {
			return err
		}

		return m.libraries.Add(entry.ID, cfg)

	case api.KindWorkflow:
		cfg, err := m.unpackWorkflow(entry.Data)
		if err != nil {
			return err
		}

		runner, err := m.compileWorkflow(entry.ID, cfg)
		if err != nil {
			return err
		}

		if err := m.workflows.Add(entry.ID, cfg, m.modules, m.libraries); err != nil {
			return err
		}

		m.registerWorkflow(ctx, entry.ID, runner)
		return nil

	case api.KindTerminal:
		cfg, err := m.unpackTerminal(entry.Data)
		if err != nil {
			return err
		}

		app, err := m.compileTerminal(entry.ID, cfg)
		if err != nil {
			return err
		}

		if err := m.terminals.Add(entry.ID, cfg, m.modules, m.libraries); err != nil {
			return err
		}

		m.registerTerminal(ctx, entry.ID, app)
		return nil

	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Update implements registry.EntryListener
func (m *RuntimeManager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required for update operation")
	}

	switch entry.Kind {
	case api.KindFunction:
		cfg, err := m.unpackFunction(entry.Data)
		if err != nil {
			return err
		}

		handler, err := m.compileFunction(entry.ID, cfg)
		if err != nil {
			return err
		}

		m.callable.Store(entry.ID, handler)
		m.registerHandler(ctx, entry.ID)

		return nil

		// todo: dep graph!
	case api.KindLibrary:
		cfg, err := m.unpackLibrary(entry.Data)
		if err != nil {
			return err
		}

		// validate dependencies can be compiled
		funcs, terminals, wfls, err := m.validateLibraryUpdateDependencies(entry.ID, cfg)
		if err != nil {
			return err
		}

		// we can update library now
		if err := m.libraries.Update(entry.ID, cfg); err != nil {
			return err
		}

		for id, fn := range funcs {
			// recompile
			callable, err := m.compileFunction(id, fn)
			if err != nil {
				return err
			}

			if err := m.functions.Update(id, fn, m.modules, m.libraries); err != nil {
				return err
			}

			m.callable.Store(id, callable)
			m.registerHandler(ctx, id)
		}

		// workflows
		for id, wf := range wfls {
			// recompile
			runner, err := m.compileWorkflow(id, wf)
			if err != nil {
				return err
			}

			if err := m.workflows.Update(id, wf, m.modules, m.libraries); err != nil {
				return err
			}

			m.registerWorkflow(ctx, id, runner)
		}

		// terminals
		for id, term := range terminals {
			// recompile
			app, err := m.compileTerminal(id, term)
			if err != nil {
				return err
			}

			if err := m.terminals.Update(id, term, m.modules, m.libraries); err != nil {
				return err
			}

			m.registerTerminal(ctx, id, app)
		}

		return nil

	case api.KindWorkflow:
		cfg, err := m.unpackWorkflow(entry.Data)
		if err != nil {
			return err
		}

		runner, err := m.compileWorkflow(entry.ID, cfg)
		if err != nil {
			return err
		}

		if err := m.workflows.Update(entry.ID, cfg, m.modules, m.libraries); err != nil {
			return err
		}

		m.registerWorkflow(ctx, entry.ID, runner)
		return nil

	case api.KindTerminal:
		cfg, err := m.unpackTerminal(entry.Data)
		if err != nil {
			return err
		}

		term, err := m.compileTerminal(entry.ID, cfg)
		if err != nil {
			return err
		}

		if err := m.terminals.Update(entry.ID, cfg, m.modules, m.libraries); err != nil {
			return err
		}

		m.registerTerminal(ctx, entry.ID, term)
		return nil
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Delete implements registry.EntryListener
func (m *RuntimeManager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.KindFunction:
		m.bus.Send(ctx, events.Event{
			System: runtime.FunctionSystem,
			Kind:   runtime.DeleteFunctionCommand,
			Path:   events.Path(entry.ID),
		})
		m.callable.Delete(entry.ID)
		return m.functions.Delete(entry.ID)

	case api.KindLibrary:
		// Check for dependent functions before deleting
		dependent := m.functions.FindDependentOnLibrary(entry.ID)
		if len(dependent) > 0 {
			return fmt.Errorf("library %s is used by functions: %v", entry.ID, dependent)
		}

		dependentWorkflows := m.workflows.FindDependentOnLibrary(entry.ID)
		if len(dependentWorkflows) > 0 {
			return fmt.Errorf("library %s is used by workflows: %v", entry.ID, dependentWorkflows)
		}

		// Check for dependent terminals
		dependentTerms := m.terminals.FindDependentOnLibrary(entry.ID)
		if len(dependentTerms) > 0 {
			return fmt.Errorf("library %s is used by terminals: %v", entry.ID, dependentTerms)
		}

		return m.libraries.Delete(entry.ID)

		// -- todo: merge
	case api.KindWorkflow:
		m.unregisterWorkflow(ctx, entry.ID)
		return m.workflows.Delete(entry.ID)
	case api.KindTerminal:
		m.bus.Send(ctx, events.Event{
			System: shell.System,
			Kind:   shell.DeleteTerminalEvent,
			Path:   events.Path(entry.ID),
			Data:   entry.ID,
		})
		return m.terminals.Delete(entry.ID)
		// -- todo: unmerge

	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}
