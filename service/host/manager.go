package host

import (
	"context"
	"fmt"
	msg "github.com/ponyruntime/pony/system/pubsub"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
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

// NewHostManager creates a new process host manager
func NewHostManager(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return &Manager{
		log: logger,
		bus: bus,
		dtt: dtt,
	}
}

// Add creates and registers a new process host
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != process.KindHost {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	cfg := new(process.EntryConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal cfg: %w", err)
	}

	cfg.InitDefaults()

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid cfg: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Create new host instance
	host := NewProcessHost(
		entry.ID,
		cfg,
		m.log,
		func(ctx context.Context) pubsub.BatchHost {
			return msg.NewHost(ctx, msg.HostConfig{
				BufferSize:      cfg.HostConfig.BufferSize,
				WorkerCount:     cfg.HostConfig.WorkerCount,
				Logger:          m.log,
				RetryTimeout:    cfg.HostConfig.RetryTimeout,
				DeliveryTimeout: cfg.HostConfig.DeliveryTimeout,
			})
		})

	// Store in hosts map
	m.hosts.Store(entry.ID, host)

	m.log.Info("process host created", zap.String("id", entry.ID.String()))

	// Register with necessary subsystems
	m.registerHost(ctx, entry.ID, host, cfg)

	return nil
}

// Update updates an existing process host
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	return fmt.Errorf("unable to update process host")
}

// Delete removes a process host
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != process.KindHost {
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
func (m *Manager) registerHost(ctx context.Context, id registry.ID, host *Host, cfg *process.EntryConfig) {
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
