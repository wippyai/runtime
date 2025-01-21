package lua

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/runtime/lua/manager"
	"sync"

	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/pool"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// RuntimeManager handles Lua runtime operations using separate managers
type RuntimeManager struct {
	log       *zap.Logger
	bus       events.Bus
	functions *manager.Functions
	libraries *manager.Libraries
	terminals *manager.Terminals
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
		functions: manager.NewFunctions(dtt, logger),
		libraries: manager.NewLibraries(dtt, logger),
		terminals: manager.NewTerminals(dtt, logger),
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
		if err := m.functions.Add(entry, m.modules, m.libraries); err != nil {
			return err
		}
		return m.compileAndRegisterFunction(ctx, entry.ID)

	case api.KindLibrary:
		return m.libraries.Add(ctx, entry)

	case api.KindTerminal:
		if err := m.terminals.Add(ctx, entry, m.modules, m.libraries); err != nil {
			return err
		}
		return m.compileAndRegisterTerminal(ctx, entry.ID)

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
		if err := m.functions.Update(entry, m.modules, m.libraries); err != nil {
			return err
		}

		return m.compileAndRegisterFunction(ctx, entry.ID)

	case api.KindLibrary:
		if err := m.libraries.Update(ctx, entry); err != nil {
			return err
		}

		// Find and recompile all dependent functions
		dependentFunctions := m.functions.FindDependentOnLibrary(entry.ID)
		for _, functionID := range dependentFunctions {
			if err := m.compileAndRegisterFunction(ctx, functionID); err != nil {
				m.log.Error("failed to recompile function after library update",
					zap.String("function", string(functionID)),
					zap.String("library", string(entry.ID)),
					zap.Error(err))
				return err
			}
		}

		// Find and update all dependent terminals
		dependentTerminals := m.terminals.FindDependentOnLibrary(entry.ID)
		for _, terminalID := range dependentTerminals {
			// Recreate and register terminal
			if err := m.compileAndRegisterTerminal(ctx, terminalID); err != nil {
				m.log.Error("failed to recreate terminal after library update",
					zap.String("terminal", string(terminalID)),
					zap.String("library", string(entry.ID)),
					zap.Error(err))
				return err
			}
		}

		return nil

	case api.KindTerminal:
		if err := m.terminals.Update(entry, m.modules, m.libraries); err != nil {
			return err
		}

		return m.compileAndRegisterTerminal(ctx, entry.ID)

	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Delete implements registry.EntryListener
func (m *RuntimeManager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.KindFunction:
		m.bus.Send(ctx, events.Event{
			System: runtime.System,
			Kind:   runtime.DeleteHandlerEvent,
			Path:   events.Path(entry.ID),
			Data:   runtime.DeleteHandler{Target: entry.ID},
		})
		m.callable.Delete(entry.ID)
		return m.functions.Delete(entry)

	case api.KindLibrary:
		// Check for dependent functions before deleting
		dependent := m.functions.FindDependentOnLibrary(entry.ID)
		if len(dependent) > 0 {
			return fmt.Errorf("library %s is used by functions: %v", entry.ID, dependent)
		}

		// Check for dependent terminals
		dependentTerms := m.terminals.FindDependentOnLibrary(entry.ID)
		if len(dependentTerms) > 0 {
			return fmt.Errorf("library %s is used by terminals: %v", entry.ID, dependentTerms)
		}

		return m.libraries.Delete(ctx, entry)

	case api.KindTerminal:
		m.bus.Send(ctx, events.Event{
			System: terminal.System,
			Kind:   terminal.DeleteTerminalEvent,
			Path:   events.Path(entry.ID),
			Data:   entry.ID,
		})

		return m.terminals.Delete(entry)

	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Execute executes a Lua function with the given arguments
func (m *RuntimeManager) Execute(task runtime.Task) (chan *runtime.Result, error) {
	// Get the callable from the sync map
	cl, ok := m.callable.Load(task.Target)
	if !ok {
		return nil, fmt.Errorf("handler not found for target: %s", task.Target)
	}

	handler, ok := cl.(api.Callable)
	if !ok {
		return nil, fmt.Errorf("handler is not a callable")
	}

	// Get the function configuration
	fn, exists := m.functions.GetFunction(task.Target)
	if !exists {
		return nil, fmt.Errorf("function configuration not found for target: %s", task.Target)
	}

	// Convert payloads to Lua values
	args := make([]lua.LValue, 0, len(task.Payloads))
	if len(task.Payloads) > 0 {
		dtt, ok := task.Context.Value(contextapi.TranscoderCtx).(payload.Transcoder)
		if !ok {
			return nil, fmt.Errorf("transcoder not found in context")
		}

		for _, p := range task.Payloads {
			local, err := dtt.Transcode(p, payload.Lua)
			if err != nil {
				return nil, fmt.Errorf("failed to transcode payload: %w", err)
			}

			args = append(args, local.Data().(lua.LValue))
		}
	}

	// Create execution context with task ID
	ctx, cancel := context.WithCancel(
		context.WithValue(task.Context, contextapi.TaskCtx, task.Target),
	)
	defer cancel()

	// Execute the function
	result, err := handler.Execute(ctx, fn.Method, args...)

	// Create result channel with buffer size 1
	resultChan := make(chan *runtime.Result, 1)
	resultChan <- &runtime.Result{
		Payload: payload.NewPayload(result, payload.Lua),
		Error:   err,
	}
	close(resultChan)

	return resultChan, nil
}

// Internal methods

func (m *RuntimeManager) compileAndRegisterFunction(ctx context.Context, id registry.ID) error {
	factory, err := m.functions.MakeFactory(id, m.modules, m.libraries, m.log)
	if err != nil {
		return fmt.Errorf("failed to create factory: %w", err)
	}

	if err := factory.Compile(); err != nil {
		return fmt.Errorf("failed to compile function: %w", err)
	}

	fn, exists := m.functions.GetFunction(id)
	if !exists {
		return fmt.Errorf("function configuration not found")
	}

	vmPool, err := m.pools.Build(factory, fn)
	if err != nil {
		return fmt.Errorf("failed to build VM pool: %w", err)
	}

	m.callable.Store(id, vmPool)

	m.bus.Send(ctx, events.Event{
		System: runtime.System,
		Kind:   runtime.RegisterHandlerEvent,
		Path:   events.Path(id),
		Data:   runtime.RegisterHandler{Target: id, Handler: m.Execute},
	})

	return nil
}

func (m *RuntimeManager) compileAndRegisterTerminal(ctx context.Context, id registry.ID) error {
	term, exists := m.terminals.GetTerminal(id)
	if !exists {
		return fmt.Errorf("terminal configuration not found")
	}

	instance, err := m.terminals.MakeTerminal(id, m.modules, m.libraries)
	if err != nil {
		return fmt.Errorf("failed to create terminal: %w", err)
	}

	m.bus.Send(ctx, events.Event{
		System: terminal.System,
		Kind:   terminal.RegisterTerminalEvent,
		Path:   events.Path(id),
		Data: terminal.TerminalApp{
			Terminal:  instance,
			Options:   term.Options,
			Lifecycle: term.Lifecycle,
		},
	})

	return nil
}
