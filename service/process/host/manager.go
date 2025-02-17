package host

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/process"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

// Manager handles process host lifecycle and registration
type Manager struct {
	log   *zap.Logger
	bus   events.Bus
	dtt   payload.Transcoder
	mu    sync.RWMutex
	hosts sync.Map // map[registry.ID]*Host
}

// NewManager creates a new process host manager
func NewManager(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return &Manager{
		log: logger,
		bus: bus,
		dtt: dtt,
	}
}

// Add creates and registers a new process host
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindHost {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	cfg := new(api.EntryConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Create new host instance
	host := NewHost(entry.ID, cfg.HostConfig, m.log.Named("host."+entry.ID.String()))

	// Store in hosts map
	m.hosts.Store(entry.ID, host)

	m.log.Info("process host created", zap.String("id", entry.ID.String()))

	// Register with necessary subsystems
	m.registerHost(ctx, entry.ID, host, cfg)

	return nil
}

// Update updates an existing process host
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindHost {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	cfg := new(api.EntryConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	hostRaw, exists := m.hosts.Load(entry.ID)
	if !exists {
		return fmt.Errorf("host %s not found", entry.ID)
	}

	host := hostRaw.(*Host)
	if err := host.UpdateConfig(ctx, cfg.HostConfig); err != nil {
		return fmt.Errorf("failed to update host config: %w", err)
	}

	// Update supervisor config
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: cfg.Lifecycle,
		},
	})

	m.log.Info("process host updated", zap.String("id", entry.ID.String()))

	return nil
}

// Delete removes a process host
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindHost {
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
func (m *Manager) registerHost(ctx context.Context, id registry.ID, host *Host, cfg *api.EntryConfig) {
	// Register with pubsub
	m.bus.Send(ctx, events.Event{
		System: pubsub.System,
		Kind:   pubsub.RegisterHost,
		Path:   id.String(),
		Data:   process.Host(host),
	})

	// Register as process host
	m.bus.Send(ctx, events.Event{
		System: process.HostSystem,
		Kind:   process.RegisterHost,
		Path:   id.String(),
		Data:   process.Managed(host),
	})

	// Register with supervisor
	m.bus.Send(ctx, events.Event{
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
	// Remove from pubsub
	m.bus.Send(ctx, events.Event{
		System: pubsub.System,
		Kind:   pubsub.DeleteHost,
		Path:   id.String(),
	})

	// Remove from process hosts
	m.bus.Send(ctx, events.Event{
		System: process.HostSystem,
		Kind:   process.DeleteHost,
		Path:   id.String(),
	})

	// Remove from supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   id.String(),
	})
}
