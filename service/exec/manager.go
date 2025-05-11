package native

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/service/exec/native"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/service/exec"
	"go.uber.org/zap"
)

// ExecutorFactoryAPI defines interface for executor factory
type ExecutorFactoryAPI interface {
	// CreateExecutor creates a new executor with the given id and configuration
	CreateExecutor(id registry.ID, cfg *exec.NativeExecutorConfig) (exec.ProcessExecutor, error)
}

// Manager handles native executor lifecycle and resource provisioning
type Manager struct {
	log       *zap.Logger
	dtt       payload.Transcoder
	bus       event.Bus
	factory   ExecutorFactoryAPI
	mu        sync.RWMutex
	executors map[registry.ID]*executorProvider
}

// NewManager creates a new native executor manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	factory := native.NewExecutorFactory(log)

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
	// Only handle native executors
	if entry.Kind != exec.KindNativeExecutor {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if executor already exists
	if _, exists := m.executors[entry.ID]; exists {
		return fmt.Errorf("executor %s already exists", entry.ID)
	}

	// Decode the configuration
	cfg := new(exec.NativeExecutorConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to decode configuration: %w", err)
	}

	// Create executor using factory
	executor, err := m.factory.CreateExecutor(entry.ID, cfg)
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
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
	if entry.Kind != exec.KindNativeExecutor {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if executor exists
	oldProvider, exists := m.executors[entry.ID]
	if !exists {
		return fmt.Errorf("executor %s not found", entry.ID)
	}

	// Decode the configuration
	cfg := new(exec.NativeExecutorConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to decode configuration: %w", err)
	}

	// Create executor using factory
	executor, err := m.factory.CreateExecutor(entry.ID, cfg)
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
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
	if entry.Kind != exec.KindNativeExecutor {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if executor exists
	provider, exists := m.executors[entry.ID]
	if !exists {
		return fmt.Errorf("executor %s not found", entry.ID)
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
