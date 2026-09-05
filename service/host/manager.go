// SPDX-License-Identifier: MPL-2.0

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
	hostapi "github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/system/entry"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"github.com/wippyai/runtime/system/scheduler/affinity"
	"go.uber.org/zap"
)

// Manager manages process host instances.
type Manager struct {
	bus             event.Bus
	dtt             payload.Transcoder
	commandRegistry dispatcherapi.Registry
	factory         process.Factory
	pidGen          process.PIDGenerator
	log             *zap.Logger
	hosts           map[registry.ID]*Host
	actorAffinity   affinity.Set
	wasmAffinity    affinity.Set
	mutationMu      sync.Mutex
	mu              sync.RWMutex
}

// SetActorAffinity pins each host's scheduler workers to the given CPU set and
// sizes the worker pool to it, keeping actor execution on cores reserved away
// from WASM. Empty (the default) leaves scheduling unpinned. Call before Add.
func (m *Manager) SetActorAffinity(set affinity.Set) {
	m.mutationMu.Lock()
	m.actorAffinity = append(affinity.Set(nil), set...)
	m.mutationMu.Unlock()
}

// SetWASMAffinity pins each WASM host's scheduler workers to the given CPU set
// and sizes the worker pool to it. Empty (the default) leaves scheduling unpinned.
// Call before Add.
func (m *Manager) SetWASMAffinity(set affinity.Set) {
	m.mutationMu.Lock()
	m.wasmAffinity = append(affinity.Set(nil), set...)
	m.mutationMu.Unlock()
}

// NewManager creates a new host manager.
func NewManager(bus event.Bus, dtt payload.Transcoder, cmdRegistry dispatcherapi.Registry, factory process.Factory, pidGen process.PIDGenerator, logger *zap.Logger) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		log:             logger,
		bus:             bus,
		dtt:             dtt,
		commandRegistry: cmdRegistry,
		factory:         factory,
		pidGen:          pidGen,
		hosts:           make(map[registry.ID]*Host),
	}
}

// Add implements registry.EntryListener.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != hostapi.Host {
		return NewUnsupportedEntryKindError(entry.Kind)
	}
	cfg, err := entryutil.DecodeEntryConfig[hostapi.EntryConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewDecodeConfigError(err)
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	h := NewHost(entry.ID, cfg, nil, m.factory, m.pidGen, m.log)

	// Create composite lifecycle: global handlers first, then host-specific
	lifecycle := &compositeLifecycle{
		global: process.GetLifecycleRegistry(ctx),
		host:   h,
	}

	opts := []actor.Option{
		actor.WithWorkers(cfg.HostConfig.Workers),
		actor.WithQueueSize(cfg.HostConfig.QueueSize),
		actor.WithLocalQueueSize(cfg.HostConfig.LocalQueueSize),
		actor.WithLifecycle(lifecycle),
	}
	if cfg.HostConfig.WorkerClass == hostapi.WorkerClassWASM {
		opts = append(opts, actor.WithDedicatedThreads())
		if len(m.wasmAffinity) > 0 {
			opts = append(opts, actor.WithWorkers(len(m.wasmAffinity)), actor.WithThreadPin(m.wasmAffinity))
			h.affinityManaged = true
		}
	} else if len(m.actorAffinity) > 0 {
		opts = append(opts, actor.WithWorkers(len(m.actorAffinity)), actor.WithThreadPin(m.actorAffinity))
		h.affinityManaged = true
	}

	scheduler := actor.NewScheduler(m.commandRegistry, opts...)
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
	if entry.Kind != hostapi.Host {
		return NewUnsupportedEntryKindError(entry.Kind)
	}

	// Validate the complete replacement before looking up or mutating the live
	// host. A malformed registry update must never take a healthy executor down.
	cfg, err := entryutil.DecodeEntryConfig[hostapi.EntryConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewDecodeConfigError(err)
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	m.mu.RLock()
	h, ok := m.hosts[entry.ID]
	m.mu.RUnlock()
	if !ok {
		return NewHostNotFoundError(entry.ID)
	}

	changed, err := h.updateConfig(cfg, h.affinityManaged)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	m.log.Info("host workers resized",
		zap.String("id", entry.ID.String()),
		zap.Int("workers", cfg.HostConfig.Workers))
	return nil
}

// Delete implements registry.EntryListener.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != hostapi.Host {
		return NewUnsupportedEntryKindError(entry.Kind)
	}
	m.mutationMu.Lock()
	m.mu.Lock()
	h, ok := m.hosts[entry.ID]
	if !ok {
		m.mu.Unlock()
		m.mutationMu.Unlock()
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
	m.mutationMu.Unlock()

	// The host is no longer discoverable and its unregister events are ordered
	// before any replacement Add. Draining can now proceed without blocking
	// unrelated manager mutations for the scheduler shutdown timeout.
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

	h, ok := m.hosts[registry.ParseID(hostID)]
	return h, ok
}

// compositeLifecycle wraps global lifecycle with host-specific handlers.
type compositeLifecycle struct {
	global process.Lifecycle
	host   process.Lifecycle
}

func (c *compositeLifecycle) OnStart(ctx context.Context, processID pid.PID, proc process.Process) error {
	if err := c.global.OnStart(ctx, processID, proc); err != nil {
		return err
	}
	if err := c.host.OnStart(ctx, processID, proc); err != nil {
		c.global.OnComplete(ctx, processID, &runtime.Result{Error: err})
		return err
	}
	return nil
}

func (c *compositeLifecycle) OnComplete(ctx context.Context, processID pid.PID, result *runtime.Result) {
	c.global.OnComplete(ctx, processID, result)
	c.host.OnComplete(ctx, processID, result)
}
