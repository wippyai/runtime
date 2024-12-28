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

// Manager handles Lua functions and libraries
type Manager struct {
	log *zap.Logger
	bus events.Bus
	dtt payload.Transcoder
	mu  sync.RWMutex

	functions map[registry.ID]*config.FunctionConfig
	libraries map[registry.ID]*config.LibraryConfig
}

// NewRuntimeManager creates a new Lua manager instance
func NewRuntimeManager(
	bus events.Bus,
	dtt payload.Transcoder,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		log:       logger,
		bus:       bus,
		dtt:       dtt,
		functions: make(map[registry.ID]*config.FunctionConfig),
		libraries: make(map[registry.ID]*config.LibraryConfig),
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
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
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
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
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case config.KindFunction:
		return m.deleteFunction(ctx, entry)
	case config.KindLibrary:
		return m.deleteLibrary(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

func (m *Manager) handleTask(task runtime.Task) (chan *runtime.Result, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *Manager) unmarshalAndValidate(data payload.Payload, cfg interface{}) error {
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

func (m *Manager) addFunction(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(config.FunctionConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.functions[entry.ID]; exists {
		return fmt.Errorf("function %s already exists", entry.ID)
	}

	m.functions[entry.ID] = cfg
	m.bus.Send(ctx, events.Event{
		System: runtime.System,
		Kind:   runtime.RegisterHandlerEvent,
		Data:   runtime.RegisterHandler{Target: entry.ID, Handler: m.handleTask},
	})

	m.log.Info("added function", zap.String("id", string(entry.ID)))

	return nil
}

func (m *Manager) updateFunction(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(config.FunctionConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.functions[entry.ID]; !exists {
		return fmt.Errorf("function %s not found", entry.ID)
	}

	m.functions[entry.ID] = cfg
	return nil
}

func (m *Manager) deleteFunction(ctx context.Context, entry registry.Entry) error {
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

func (m *Manager) addLibrary(_ context.Context, entry registry.Entry) error {
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

func (m *Manager) updateLibrary(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := new(config.LibraryConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.libraries[entry.ID]; !exists {
		return fmt.Errorf("library %s not found", entry.ID)
	}

	m.libraries[entry.ID] = cfg
	return nil
}

func (m *Manager) deleteLibrary(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.libraries[entry.ID]; !exists {
		return fmt.Errorf("library %s not found", entry.ID)
	}

	delete(m.libraries, entry.ID)
	return nil
}
