package host2

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	procapi "github.com/wippyai/runtime/api/process"
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

	mu    sync.RWMutex
	hosts map[registry.ID]*Host
}

// NewManager creates a new host2 manager.
func NewManager(bus event.Bus, dtt payload.Transcoder, cmdRegistry *actor.Registry, logger *zap.Logger) *Manager {
	return &Manager{
		log:             logger.Named("host2"),
		bus:             bus,
		dtt:             dtt,
		commandRegistry: cmdRegistry,
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
		actor.WithQueueSize(cfg.HostConfig.BufferSize),
		actor.WithOnComplete(func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			// Execute OnComplete hooks stored in context
			if hooks := procapi.GetOnCompleteHooks(ctx); len(hooks) > 0 {
				for _, hook := range hooks {
					hook(ctx, pid, result)
				}
			}
		}),
	)

	h := NewHost(entry.ID, cfg, scheduler, m.log)

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

	// Register with process host system
	m.bus.Send(ctx, event.Event{
		System: procapi.HostSystem,
		Kind:   procapi.HostRegister,
		Path:   entry.ID.String(),
		Data:   procapi.Managed(h),
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

	// Unregister from process host system
	m.bus.Send(ctx, event.Event{
		System: procapi.HostSystem,
		Kind:   procapi.HostDelete,
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
