package terminal

import (
	"context"
	"sync"

	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/service/terminal"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"github.com/wippyai/runtime/system/logs"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// Manager manages terminal host instances.
type Manager struct {
	log             *zap.Logger
	bus             event.Bus
	dtt             payload.Transcoder
	commandRegistry dispatcherapi.Registry
	factory         process.Factory

	mu    sync.RWMutex
	hosts map[registry.ID]*Host
}

// NewManager creates a new terminal manager.
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	cmdRegistry dispatcherapi.Registry,
	factory process.Factory,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		log:             logger,
		bus:             bus,
		dtt:             dtt,
		commandRegistry: cmdRegistry,
		factory:         factory,
		hosts:           make(map[registry.ID]*Host),
	}
}

// Add implements registry.EntryListener.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	cfg, err := entryutil.DecodeEntryConfig[terminal.HostConfig](ctx, m.dtt, entry)
	if err != nil {
		return newDecodeConfigError(err)
	}

	logCtrl := logs.NewConfigSwitcher(m.bus, m.log)

	// Create host first, then scheduler with host as lifecycle
	h := NewHost(entry.ID, cfg, nil, m.factory, logCtrl, m.log)
	scheduler := actor.NewScheduler(m.commandRegistry,
		actor.WithWorkers(1),
		actor.WithLifecycle(h),
	)
	h.scheduler = scheduler

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

	m.log.Info("terminal host added", zap.String("id", entry.ID.String()))
	return nil
}

// Update implements registry.EntryListener.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
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

	m.log.Info("terminal host deleted", zap.String("id", entry.ID.String()))
	return nil
}

// GetHost returns a host by ID.
func (m *Manager) GetHost(hostID string) (process.Host, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, h := range m.hosts {
		if id.String() == hostID {
			return h, true
		}
	}
	return nil, false
}
