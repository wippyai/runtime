package host

import (
	"context"
	"fmt"
	"sync"

	supervisor2 "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/service/host"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

// Manager handles process host lifecycle and registration
type Manager struct {
	log         *zap.Logger
	bus         event.Bus
	dtt         payload.Transcoder
	mu          sync.RWMutex
	hosts       sync.Map // map[registry.Source]*Host
	hostFactory Factory
}

// NewHostManager creates a new process host manager with default host factory
func NewHostManager(bus event.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return NewHostManagerWithFactory(bus, dtt, logger, NewDefaultHostFactory())
}

// NewHostManagerWithFactory creates a new process host manager with a custom host factory
func NewHostManagerWithFactory(
	bus event.Bus,
	dtt payload.Transcoder,
	logger *zap.Logger,
	factory Factory,
) *Manager {
	return &Manager{
		log:         logger,
		bus:         bus,
		dtt:         dtt,
		hostFactory: factory,
	}
}

// Add creates and registers a new process host
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != host.KindHost {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	cfg := new(host.EntryConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal cfg: %w", err)
	}

	cfg.InitDefaults()

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid cfg: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Create new host instance using the factory
	h, err := m.hostFactory.CreateHost(entry.ID, cfg, m.log)
	if err != nil {
		return fmt.Errorf("failed to create host: %w", err)
	}

	// Convert to concrete type for our internal storage
	// This is safe because we know our factory returns a *Host
	concreteHost, ok := h.(*Host)
	if !ok {
		return fmt.Errorf("factory returned unexpected host type: %T", h)
	}

	// Store in hosts map
	m.hosts.Store(entry.ID, concreteHost)

	m.log.Info("process host created", zap.String("id", entry.ID.String()))

	// Register with necessary subsystems
	m.registerHost(ctx, entry.ID, concreteHost, cfg)

	return nil
}

// Delete removes a process host
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != host.KindHost {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.removeHost(ctx, entry.ID)
	m.hosts.Delete(entry.ID)

	m.log.Info("process host removed", zap.String("id", entry.ID.String()))

	return nil
}

// registerHost registers the process host with necessary subsystems
func (m *Manager) registerHost(ctx context.Context, id registry.ID, host *Host, cfg *host.EntryConfig) {
	// Register with pubsub
	m.bus.Send(ctx, event.Event{
		System: pubsub.System,
		Kind:   pubsub.HostRegister,
		Path:   id.String(),
		Data:   supervisor2.Host(host),
	})

	// Register as process host
	m.bus.Send(ctx, event.Event{
		System: supervisor2.HostSystem,
		Kind:   supervisor2.HostRegister,
		Path:   id.String(),
		Data:   supervisor2.Managed(host),
	})

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   id.String(),
		Data: &supervisor.Entry{
			Service: host,
			Config:  cfg.Lifecycle,
		},
	})
}

// removeHost removes the process host from all subsystems
func (m *Manager) removeHost(ctx context.Context, id registry.ID) {
	// Done from pubsub
	m.bus.Send(ctx, event.Event{
		System: pubsub.System,
		Kind:   pubsub.HostDelete,
		Path:   id.String(),
	})

	// Done from process hosts
	m.bus.Send(ctx, event.Event{
		System: supervisor2.HostSystem,
		Kind:   supervisor2.HostDelete,
		Path:   id.String(),
	})

	// Done from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   id.String(),
	})
}

// Update updates an existing process host
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	return fmt.Errorf("unable to update process host")
}
