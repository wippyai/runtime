package host

import (
	"context"
	"sync"

	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
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
	commandRegistry dispatcherapi.Registry
	factory         process.Factory

	mu    sync.RWMutex
	hosts map[registry.ID]*Host
}

// NewManager creates a new host manager.
func NewManager(bus event.Bus, dtt payload.Transcoder, cmdRegistry dispatcherapi.Registry, factory process.Factory, logger *zap.Logger) *Manager {
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
	cfg, err := entryutil.DecodeEntryConfig[host.EntryConfig](ctx, m.dtt, entry)
	if err != nil {
		return newDecodeConfigError(err)
	}

	h := NewHost(entry.ID, cfg, nil, m.factory, m.log)

	// Create composite lifecycle: global handlers first, then host-specific
	lifecycle := &compositeLifecycle{
		global: process.GetLifecycleRegistry(ctx),
		host:   h,
	}

	scheduler := actor.NewScheduler(m.commandRegistry,
		actor.WithWorkers(cfg.HostConfig.Workers),
		actor.WithQueueSize(cfg.HostConfig.QueueSize),
		actor.WithLocalQueueSize(cfg.HostConfig.LocalQueueSize),
		actor.WithLifecycle(lifecycle),
	)
	h.scheduler = scheduler

	m.mu.Lock()
	m.hosts[entry.ID] = h
	m.mu.Unlock()

	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostRegister,
		Path:   entry.ID.String(),
		Data:   relay.Receiver(h),
	})

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
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

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})

	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostDelete,
		Path:   entry.ID.String(),
	})

	if err := h.Stop(ctx); err != nil {
		m.log.Error("failed to stop host", zap.Error(err))
	}

	m.log.Info("host deleted", zap.String("id", entry.ID.String()))
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

// compositeLifecycle wraps global lifecycle with host-specific handlers.
type compositeLifecycle struct {
	global process.Lifecycle
	host   process.Lifecycle
}

func (c *compositeLifecycle) OnStart(ctx context.Context, processID pid.PID, proc process.Process) {
	if c.global != nil {
		c.global.OnStart(ctx, processID, proc)
	}
	if c.host != nil {
		c.host.OnStart(ctx, processID, proc)
	}
}

func (c *compositeLifecycle) OnComplete(ctx context.Context, processID pid.PID, result *runtime.Result) {
	if c.global != nil {
		c.global.OnComplete(ctx, processID, result)
	}
	if c.host != nil {
		c.host.OnComplete(ctx, processID, result)
	}
}
