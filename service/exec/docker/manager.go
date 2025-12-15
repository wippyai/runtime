package docker

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
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

// ExecutorFactoryAPI defines interface for executor factory
type ExecutorFactoryAPI interface {
	CreateExecutor(id registry.ID, cfg *execapi.DockerExecutorConfig) (execapi.ProcessExecutor, error)
}

// Manager handles Docker executor lifecycle and resource provisioning
type Manager struct {
	log       *zap.Logger
	dtt       payload.Transcoder
	bus       event.Bus
	factory   ExecutorFactoryAPI
	mu        sync.RWMutex
	executors map[registry.ID]*executorProvider
}

// NewManager creates a new Docker executor manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	factory := NewExecutorFactory(log)

	return &Manager{
		log:       log,
		dtt:       dtt,
		bus:       bus,
		factory:   factory,
		executors: make(map[registry.ID]*executorProvider),
	}
}

// Add implements registry.EntryListener for Docker executors
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != execapi.DockerExecutor {
		return serviceexec.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.executors[entry.ID]; exists {
		return serviceexec.NewExecutorAlreadyExistsError(entry.ID.String())
	}

	cfg, err := entryutil.DecodeEntryConfig[execapi.DockerExecutorConfig](ctx, m.dtt, entry)
	if err != nil {
		return serviceexec.NewConfigDecodeError(err)
	}

	executor, err := m.factory.CreateExecutor(entry.ID, cfg)
	if err != nil {
		return serviceexec.NewExecutorCreateError(err)
	}

	provider := newExecutorProvider(executor)
	m.executors[entry.ID] = provider

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

	m.log.Info("added docker executor",
		zap.String("id", entry.ID.String()),
		zap.String("image", cfg.Image))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != execapi.DockerExecutor {
		return serviceexec.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	oldProvider, exists := m.executors[entry.ID]
	if !exists {
		return serviceexec.NewExecutorNotFoundError(entry.ID.String())
	}

	cfg, err := entryutil.DecodeEntryConfig[execapi.DockerExecutorConfig](ctx, m.dtt, entry)
	if err != nil {
		return serviceexec.NewConfigDecodeError(err)
	}

	executor, err := m.factory.CreateExecutor(entry.ID, cfg)
	if err != nil {
		return serviceexec.NewExecutorCreateError(err)
	}

	provider := newExecutorProvider(executor)

	if err := oldProvider.Close(); err != nil {
		m.log.Warn("error closing old executor provider",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
	}

	m.executors[entry.ID] = provider

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

	m.log.Info("updated docker executor",
		zap.String("id", entry.ID.String()))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != execapi.DockerExecutor {
		return serviceexec.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	provider, exists := m.executors[entry.ID]
	if !exists {
		return serviceexec.NewExecutorNotFoundError(entry.ID.String())
	}

	if err := provider.Close(); err != nil {
		m.log.Warn("error closing executor provider",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
	}

	delete(m.executors, entry.ID)

	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	m.log.Info("deleted docker executor",
		zap.String("id", entry.ID.String()))

	return nil
}

// executorProvider implements resource.Provider for Docker executors
type executorProvider struct {
	executor execapi.ProcessExecutor
	mu       sync.RWMutex
	closed   bool
}

func newExecutorProvider(executor execapi.ProcessExecutor) *executorProvider {
	return &executorProvider{executor: executor}
}

func (p *executorProvider) Acquire(_ context.Context, _ registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, systemresource.ErrClosed
	}

	if mode == resource.ModeExclusive {
		return nil, systemresource.ErrLocked
	}

	return &executorResource{executor: p.executor}, nil
}

func (p *executorProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true

	if closer, ok := p.executor.(interface{ Close() error }); ok {
		return closer.Close()
	}

	return nil
}

type executorResource struct {
	executor execapi.ProcessExecutor
	mu       sync.Mutex
	released bool
}

func (r *executorResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.released {
		return nil, resource.ErrReleased
	}

	return r.executor, nil
}

func (r *executorResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.released = true
}
