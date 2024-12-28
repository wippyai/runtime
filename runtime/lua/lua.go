package lua

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"
	config "github.com/ponyruntime/pony/api/runtime/lua"
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
	functions map[registry.ID]*config.FunctionConfig
	libraries map[registry.ID]*config.LibraryConfig
	modules   map[string]config.Module

	callable sync.Map
}

// NewRuntimeManager creates a new Lua manager instance
func NewRuntimeManager(
	bus events.Bus,
	dtt payload.Transcoder,
	logger *zap.Logger,
	modules ...config.Module,
) *RuntimeManager {
	m := &RuntimeManager{
		log:       logger,
		bus:       bus,
		dtt:       dtt,
		functions: make(map[registry.ID]*config.FunctionConfig),
		libraries: make(map[registry.ID]*config.LibraryConfig),
		modules:   make(map[string]config.Module),
		callable:  sync.Map{},
	}

	for _, module := range modules {
		m.modules[module.Name()] = module
	}

	return m
}

// Add implements registry.EntryListener
func (m *RuntimeManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required for create operation")
	}

	switch entry.Kind {
	case config.KindFunction:
		return m.addFunction(ctx, entry)
	case config.KindLibrary:
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
	case config.KindFunction:
		return m.updateFunction(ctx, entry)
	case config.KindLibrary:
		return m.updateLibrary(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Delete implements registry.EntryListener
func (m *RuntimeManager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case config.KindFunction:
		return m.deleteFunction(ctx, entry)
	case config.KindLibrary:
		return m.deleteLibrary(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

func (m *RuntimeManager) Execute(task runtime.Task) (chan *runtime.Result, error) {
	_, ok := m.callable.Load(task.Target)
	if !ok {
		return nil, fmt.Errorf("handler not found")
	}

	return nil, fmt.Errorf("not implemented")
}

func (m *RuntimeManager) unmarshalAndValidate(data payload.Payload, cfg interface{}) error {
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

func (m *RuntimeManager) addFunction(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(config.FunctionConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.functions[entry.ID]; exists {
		return fmt.Errorf("function %s already exists", entry.ID)
	}

	// -- compile function
	// -- end of function compilation

	m.functions[entry.ID] = cfg
	m.bus.Send(ctx, events.Event{
		System: runtime.System,
		Kind:   runtime.RegisterHandlerEvent,
		Data:   runtime.RegisterHandler{Target: entry.ID, Handler: m.Execute},
	})

	m.log.Info("added function", zap.String("id", string(entry.ID)))

	return nil
}

func (m *RuntimeManager) updateFunction(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(config.FunctionConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.functions[entry.ID]; !exists {
		return fmt.Errorf("function %s not found", entry.ID)
	}

	// -- compile function
	// -- end of function compilation

	m.functions[entry.ID] = cfg
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

	delete(m.functions, entry.ID)
	return nil
}

func (m *RuntimeManager) addLibrary(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(config.LibraryConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.libraries[entry.ID]; exists {
		return fmt.Errorf("library %s already exists", entry.ID)
	}

	m.libraries[entry.ID] = cfg
	return nil
}

func (m *RuntimeManager) updateLibrary(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(config.LibraryConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.libraries[entry.ID]; !exists {
		return fmt.Errorf("library %s not found", entry.ID)
	}

	// -- recompile dependent functions
	// -- end of recompilation

	m.libraries[entry.ID] = cfg
	return nil
}

func (m *RuntimeManager) deleteLibrary(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.libraries[entry.ID]; !exists {
		return fmt.Errorf("library %s not found", entry.ID)
	}

	// -- check if any functions depend on this library
	// -- end of check

	delete(m.libraries, entry.ID)
	return nil
}
