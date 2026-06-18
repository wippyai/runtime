// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"sync"

	envapi "github.com/wippyai/runtime/api/env"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

// Manager handles SQL database connections lifecycle and resource provisioning
type Manager struct {
	dtt      payload.Transcoder
	bus      event.Bus
	factory  Factory
	env      envapi.Registry
	log      *zap.Logger
	services map[registry.ID]*ConnPool
	mu       sync.RWMutex
}

// NewManager creates a new SQL service manager
func NewManager(
	dtt payload.Transcoder,
	bus event.Bus,
	log *zap.Logger,
	envRegistry envapi.Registry,
) (*Manager, error) {
	return NewManagerWithFactory(dtt, bus, log, envRegistry, NewDefaultPoolFactory())
}

// NewManagerWithFactory creates a new SQL service manager with the specified pool factory
func NewManagerWithFactory(
	dtt payload.Transcoder,
	bus event.Bus,
	log *zap.Logger,
	envRegistry envapi.Registry,
	factory Factory,
) (*Manager, error) {
	if dtt == nil {
		return nil, ErrTranscoderRequired
	}
	if bus == nil {
		return nil, ErrEventBusRequired
	}
	if factory == nil {
		return nil, ErrPoolFactoryRequired
	}
	if log == nil {
		log = zap.NewNop()
	}

	return &Manager{
		log:      log,
		dtt:      dtt,
		bus:      bus,
		factory:  factory,
		services: make(map[registry.ID]*ConnPool),
		env:      envRegistry,
	}, nil
}

// deps bundles the manager's collaborators for the engine lifecycle.
func (m *Manager) deps() EngineDeps {
	return EngineDeps{Transcoder: m.dtt, Env: m.env, Log: m.log}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := engineFor(entry.Kind); !ok {
		return NewUnsupportedEntryKindError(entry.Kind)
	}

	if _, exists := m.services[entry.ID]; exists {
		return NewServiceExistsError(entry.ID)
	}

	pool, cfg, err := m.factory.CreatePool(ctx, m.deps(), entry)
	if err != nil {
		return err
	}

	return m.registerService(ctx, entry, pool, cfg.LifecycleConfig())
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := engineFor(entry.Kind); !ok {
		return NewUnsupportedEntryKindError(entry.Kind)
	}

	pool, exists := m.services[entry.ID]
	if !exists {
		return NewServiceNotFoundError(entry.ID)
	}

	cfg, err := m.factory.UpdatePool(ctx, m.deps(), pool, entry)
	if err != nil {
		return err
	}

	m.updateService(ctx, entry, cfg.LifecycleConfig())
	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.services[entry.ID]
	if !exists {
		return NewServiceNotFoundError(entry.ID)
	}

	m.unregisterService(ctx, entry)
	delete(m.services, entry.ID)
	return nil
}

// registerService handles the common service registration logic
func (m *Manager) registerService(ctx context.Context, entry registry.Entry, pool *ConnPool, lifecycle supervisor.LifecycleConfig) error {
	m.services[entry.ID] = pool

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: pool,
			Config:  lifecycle,
		},
	})

	// Register as resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: pool,
			Meta:     map[string]any{"type": entry.Kind},
		},
	})

	m.log.Info("added database service",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind))

	return nil
}

// updateService handles the common service update logic
func (m *Manager) updateService(ctx context.Context, entry registry.Entry, lifecycle supervisor.LifecycleConfig) {
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: lifecycle,
		},
	})

	m.log.Info("updated database service",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind))
}

// unregisterService handles the common service unregistration logic
func (m *Manager) unregisterService(ctx context.Context, entry registry.Entry) {
	// Delete from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})

	// Delete resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	m.log.Info("removed database service",
		zap.String("id", entry.ID.String()))
}
