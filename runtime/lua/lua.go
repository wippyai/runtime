package lua

import (
	"context"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	config "github.com/ponyruntime/pony/runtime/lua/engine/config"
	"github.com/ponyruntime/pony/runtime/lua/pool"
	"github.com/yuin/gopher-lua"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// RuntimeManager handles Lua functions and libraries
type RuntimeManager struct {
	log       *zap.Logger
	bus       events.Bus
	dtt       payload.Transcoder
	mu        sync.RWMutex
	functions map[registry.ID]*api.FunctionConfig
	libraries map[registry.ID]*api.LibraryConfig
	modules   map[string]api.Module

	compiler *pool.Compiler
	callable sync.Map
}

// NewRuntimeManager creates a new Lua manager instance
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
		functions: make(map[registry.ID]*api.FunctionConfig),
		libraries: make(map[registry.ID]*api.LibraryConfig),
		modules:   make(map[string]api.Module),
		callable:  sync.Map{},
	}

	for _, module := range modules {
		m.modules[module.Name()] = module
		logger.Debug("registered module", zap.String("name", module.Name()))
	}
	m.compiler = pool.NewCompiler(logger.Named("compiler"))

	return m
}

// Add implements registry.EntryListener
func (m *RuntimeManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required for create operation")
	}

	switch entry.Kind {
	case api.KindFunction:
		return m.addFunction(ctx, entry)
	case api.KindLibrary:
		return m.addLibrary(ctx, entry)
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
		return m.updateFunction(ctx, entry)
	case api.KindLibrary:
		return m.updateLibrary(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Delete implements registry.EntryListener
func (m *RuntimeManager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.KindFunction:
		return m.deleteFunction(ctx, entry)
	case api.KindLibrary:
		return m.deleteLibrary(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

func (m *RuntimeManager) Execute(task runtime.Task) (chan *runtime.Result, error) {
	cl, ok := m.callable.Load(task.Target)
	if !ok {
		return nil, fmt.Errorf("handler not found")
	}

	handler, ok := cl.(api.Callable)
	if !ok {
		return nil, fmt.Errorf("handler is not a callable")
	}

	args := make([]lua.LValue, 0)
	if len(task.Payloads) != 0 {
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

	// to clean up all dangling references
	ctx, cancel := context.WithCancel(
		context.WithValue(task.Context, "function", task.Target),
	)
	defer cancel()

	result, err := handler.Execute(ctx, m.functions[task.Target].Method, args...)

	resultChan := make(chan *runtime.Result, 1)
	resultChan <- &runtime.Result{
		Payload: payload.NewPayload(result, payload.Lua),
		Error:   err,
	}
	close(resultChan)

	return resultChan, nil
}

func (m *RuntimeManager) unmarshalAndValidate(data payload.Payload, cfg interface{}) error {
	if err := m.dtt.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal api: %w", err)
	}

	if validator, ok := cfg.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return nil
}

func (m *RuntimeManager) addFunction(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(api.FunctionConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.functions[entry.ID]; exists {
		return fmt.Errorf("function %s already exists", entry.ID)
	}

	for _, module := range cfg.Modules {
		if _, exists := m.modules[module]; !exists {
			return fmt.Errorf("module %s not found", module)
		}
	}

	for _, library := range cfg.Libraries {
		if _, exists := m.libraries[registry.ID(library)]; !exists {
			return fmt.Errorf("library %s not found", library)
		}
	}

	btc, err := m.compileFunction(entry.ID, cfg) // todo: delegate to services
	if err != nil {
		return fmt.Errorf("failed to compile function: %w", err)
	}

	m.callable.Store(entry.ID, btc)
	m.functions[entry.ID] = cfg

	m.bus.Send(ctx, events.Event{
		System: runtime.System,
		Kind:   runtime.RegisterHandlerEvent,
		Data:   runtime.RegisterHandler{Target: entry.ID, Handler: m.Execute},
	})

	m.log.Info("added function", zap.String("id", string(entry.ID)))

	return nil
}

func (m *RuntimeManager) compileFunction(id registry.ID, cfg *api.FunctionConfig) (api.Callable, error) {
	factory, err := m.factory(id, cfg)
	if err != nil {
		return nil, err
	}

	fn, err := m.compiler.Compile(factory, cfg)
	if err != nil {
		return nil, err
	}

	return fn, nil
}

func (m *RuntimeManager) updateFunction(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(api.FunctionConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.functions[entry.ID]; !exists {
		return fmt.Errorf("function %s not found", entry.ID)
	}

	for _, module := range cfg.Modules {
		if _, exists := m.modules[module]; !exists {
			return fmt.Errorf("module %s not found", module)
		}
	}

	for _, library := range cfg.Libraries {
		if _, exists := m.libraries[registry.ID(library)]; !exists {
			return fmt.Errorf("library %s not found", library)
		}
	}

	btc, err := m.compileFunction(entry.ID, cfg)
	if err != nil {
		return fmt.Errorf("failed to compile function: %w", err)
	}

	m.callable.Store(entry.ID, btc)
	m.functions[entry.ID] = cfg

	m.log.Info("updated function", zap.String("id", string(entry.ID)))

	return nil
}

func (m *RuntimeManager) deleteFunction(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.functions[entry.ID]; !exists {
		return fmt.Errorf("function %s not found", entry.ID)
	}

	m.bus.Send(ctx, events.Event{
		System: runtime.System,
		Kind:   runtime.DeleteHandlerEvent,
		Data:   runtime.DeleteHandler{Target: entry.ID},
	})

	m.callable.Delete(entry.ID)
	delete(m.functions, entry.ID)

	m.log.Info("deleted function", zap.String("id", string(entry.ID)))

	return nil
}

func (m *RuntimeManager) addLibrary(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(api.LibraryConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.libraries[entry.ID]; exists {
		return fmt.Errorf("library %s already exists", entry.ID)
	}

	m.libraries[entry.ID] = cfg
	m.log.Info("added library", zap.String("id", string(entry.ID)))

	return nil
}

func (m *RuntimeManager) updateLibrary(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(api.LibraryConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.libraries[entry.ID]; !exists {
		return fmt.Errorf("library %s not found", entry.ID)
	}

	// -- todo: recompile dependent functions
	// -- end of recompilation

	m.libraries[entry.ID] = cfg

	m.log.Info("updated library", zap.String("id", string(entry.ID)))

	return nil
}

func (m *RuntimeManager) deleteLibrary(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.libraries[entry.ID]; !exists {
		return fmt.Errorf("library %s not found", entry.ID)
	}

	// -- todo: check if any functions depend on this library
	// -- end of check

	delete(m.libraries, entry.ID)
	return nil
}

func (m *RuntimeManager) factory(id registry.ID, fn *api.FunctionConfig) (api.Factory, error) {
	// Create new Callable api with manager's logger
	cfg := config.NewVMConfig(m.log.Named(fmt.Sprintf("vm.%s", id)))

	// Add required modules
	for _, moduleName := range fn.Modules {
		module, exists := m.modules[moduleName]
		if !exists {
			return nil, fmt.Errorf("module %s not found", moduleName)
		}

		cfg.Modules = append(cfg.Modules, module)
	}

	// Add required libraries
	for _, libID := range fn.Libraries {
		lib, exists := m.libraries[registry.ID(libID)]
		if !exists {
			return nil, fmt.Errorf("library %s not found", libID)
		}

		cfg.Libraries = append(cfg.Libraries, config.Library{
			Name:   libID,
			Script: lib.Source,
		})
	}

	// Add the function itself
	cfg.Functions = append(cfg.Functions, config.Function{
		Name:   fn.Method,
		Script: fn.Source,
	})

	return cfg, nil
}
