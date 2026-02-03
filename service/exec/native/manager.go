package native

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	execapi "github.com/wippyai/runtime/api/service/exec"
	entryutil "github.com/wippyai/runtime/internal/entry"
	serviceexec "github.com/wippyai/runtime/service/exec"
	"go.uber.org/zap"
)

// ExecutorFactoryAPI defines interface for executor factory
type ExecutorFactoryAPI interface {
	// CreateExecutor creates a new executor with the given ID and configuration
	CreateExecutor(id registry.ID, cfg *execapi.NativeExecutorConfig) (execapi.ProcessExecutor, error)
}

// Manager handles native executor lifecycle and resource provisioning
type Manager struct {
	dtt       payload.Transcoder
	bus       event.Bus
	factory   ExecutorFactoryAPI
	log       *zap.Logger
	executors map[registry.ID]*executorProvider
	mu        sync.RWMutex
}

// NewManager creates a new native executor manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	factory := NewExecutorFactory(log)

	return &Manager{
		log:       log,
		dtt:       dtt,
		bus:       bus,
		factory:   factory,
		executors: make(map[registry.ID]*executorProvider),
	}
}

// RegisterFactory allows custom factory registration (useful for testing)
func (m *Manager) RegisterFactory(factory ExecutorFactoryAPI) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.factory = factory
}

// Add implements registry.EntryListener for native executors only
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != execapi.NativeExecutor {
		return serviceexec.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.executors[entry.ID]; exists {
		return serviceexec.NewExecutorAlreadyExistsError(entry.ID.String())
	}

	cfg, err := entryutil.DecodeEntryConfig[execapi.NativeExecutorConfig](ctx, m.dtt, entry)
	if err != nil {
		return serviceexec.NewConfigDecodeError(err)
	}

	executor, err := m.factory.CreateExecutor(entry.ID, cfg)
	if err != nil {
		return serviceexec.NewExecutorCreateError(err)
	}

	// Create resource provider
	provider := newExecutorProvider(executor)

	// Store the executor
	m.executors[entry.ID] = provider

	// Register as resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: provider,
			Meta:     entry.Meta,
		},
	})

	m.log.Info("added native executor",
		zap.String("id", entry.ID.String()))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != execapi.NativeExecutor {
		return serviceexec.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	oldProvider, exists := m.executors[entry.ID]
	if !exists {
		return serviceexec.NewExecutorNotFoundError(entry.ID.String())
	}

	cfg, err := entryutil.DecodeEntryConfig[execapi.NativeExecutorConfig](ctx, m.dtt, entry)
	if err != nil {
		return serviceexec.NewConfigDecodeError(err)
	}

	executor, err := m.factory.CreateExecutor(entry.ID, cfg)
	if err != nil {
		return serviceexec.NewExecutorCreateError(err)
	}

	// Create resource provider
	provider := newExecutorProvider(executor)

	// Close old provider
	if err := oldProvider.Close(); err != nil {
		m.log.Warn("error closing old executor provider",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
	}

	// Update executor
	m.executors[entry.ID] = provider

	// Update resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: provider,
			Meta:     entry.Meta,
		},
	})

	m.log.Info("updated native executor",
		zap.String("id", entry.ID.String()))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != execapi.NativeExecutor {
		return serviceexec.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	provider, exists := m.executors[entry.ID]
	if !exists {
		return serviceexec.NewExecutorNotFoundError(entry.ID.String())
	}

	// Close provider
	if err := provider.Close(); err != nil {
		m.log.Warn("error closing executor provider",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
	}

	// Remove from managed executors
	delete(m.executors, entry.ID)

	// Unregister resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	m.log.Info("deleted native executor",
		zap.String("id", entry.ID.String()))

	return nil
}
