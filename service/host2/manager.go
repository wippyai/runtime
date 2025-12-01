package host2

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// Manager manages process.host v2 instances.
type Manager struct {
	log             *zap.Logger
	bus             event.Bus
	dtt             payload.Transcoder
	commandRegistry *actor.Registry
	factory         process2.Factory

	mu    sync.RWMutex
	hosts map[registry.ID]*Host
}

// NewManager creates a new host2 manager.
func NewManager(bus event.Bus, dtt payload.Transcoder, cmdRegistry *actor.Registry, factory process2.Factory, logger *zap.Logger) *Manager {
	return &Manager{
		log:             logger.Named("host2"),
		bus:             bus,
		dtt:             dtt,
		commandRegistry: cmdRegistry,
		factory:         factory,
		hosts:           make(map[registry.ID]*Host),
	}
}

// Add implements registry.EntryListener.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	cfg, err := entryutil.DecodeEntryConfig[host.EntryConfig](ctx, m.dtt, entry)
	if err != nil {
		return fmt.Errorf("decode config: %w", err)
	}

	// Create scheduler for this host with lifecycle callbacks
	scheduler := actor.NewScheduler(m.commandRegistry,
		actor.WithWorkers(cfg.HostConfig.Workers),
		actor.WithQueueSize(cfg.HostConfig.QueueSize),
		actor.WithLocalQueueSize(cfg.HostConfig.LocalQueueSize),
		actor.WithOnComplete(func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			// Execute OnComplete hooks stored in context
			if hooks := process2.GetOnCompleteHooks(ctx); len(hooks) > 0 {
				for _, hook := range hooks {
					hook(ctx, pid, result)
				}
			}
		}),
	)

	h := NewHost(entry.ID, cfg, scheduler, m.factory, m.log)

	m.mu.Lock()
	m.hosts[entry.ID] = h
	m.mu.Unlock()

	// Register with relay system
	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostRegister,
		Path:   entry.ID.String(),
		Data:   relay.Host(h),
	})

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: h,
			Config:  cfg.Lifecycle,
		},
	})

	m.log.Info("host added", zap.String("id", entry.ID.String()))
	return nil
}

// Update implements registry.EntryListener.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	// For now, just recreate
	if err := m.Delete(ctx, entry); err != nil {
		return err
	}
	return m.Add(ctx, entry)
}

// Delete implements registry.EntryListener.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	h, ok := m.hosts[entry.ID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.hosts, entry.ID)
	m.mu.Unlock()

	// Unregister from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   entry.ID.String(),
	})

	// Unregister from relay
	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostDelete,
		Path:   entry.ID.String(),
	})

	// Stop the host
	if err := h.Stop(ctx); err != nil {
		m.log.Error("failed to stop host", zap.Error(err))
	}

	m.log.Info("host deleted", zap.String("id", entry.ID.String()))
	return nil
}

// GetHost returns a host by ID.
func (m *Manager) GetHost(hostID string) (process2.Host, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, h := range m.hosts {
		if id.String() == hostID {
			return h, true
		}
	}
	return nil, false
}
