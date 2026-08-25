// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	memstore "github.com/wippyai/runtime/api/service/store/memory"
	"github.com/wippyai/runtime/api/supervisor"
	storesvc "github.com/wippyai/runtime/service/store"
	entryutil "github.com/wippyai/runtime/system/entry"
	"go.uber.org/zap"
)

// Manager handles memory store lifecycle and resource provisioning
type Manager struct {
	dtt    payload.Transcoder
	bus    event.Bus
	log    *zap.Logger
	coll   metrics.Collector
	stores map[registry.ID]*Store
	mu     sync.RWMutex
}

// NewManager creates a new memory store manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
	coll metrics.Collector,
) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{
		log:    log,
		dtt:    dtt,
		bus:    bus,
		coll:   coll,
		stores: make(map[registry.ID]*Store),
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != memstore.KV {
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.stores[entry.ID]; exists {
		return storesvc.NewStoreAlreadyExistsError(entry.ID.String())
	}

	// Decode and initialize configuration
	cfg, err := entryutil.DecodeEntryConfig[memstore.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// Create memory store
	store := NewStore(entry.ID, cfg, m.log)
	store.coll = m.coll
	m.stores[entry.ID] = store

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: store,
			Config:  cfg.Lifecycle,
		},
	})

	// Register as resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: store,
			Meta:     entry.Meta,
		},
	})

	m.log.Info("added memory store",
		zap.String("id", entry.ID.String()),
		zap.Int("max_size", cfg.MaxSize))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != memstore.KV {
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	store, exists := m.stores[entry.ID]
	if !exists {
		return storesvc.NewStoreNotFoundError(entry.ID.String())
	}

	// Decode and initialize updated configuration
	cfg, err := entryutil.DecodeEntryConfig[memstore.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// Running store configuration cannot change in place, so the entry update
	// installs a replacement. The superseded store keeps serving acquisitions
	// until the supervisor retires it, so no acquisition lands on a stopped
	// store.
	newStore := NewStore(entry.ID, cfg, m.log)
	newStore.coll = m.coll
	m.stores[entry.ID] = newStore

	// Point the resource registry at the replacement and wait for it to take
	// effect, so consumers are served the replacement before the supervisor
	// retires the superseded store on commit.
	if err := storesvc.SendAndAwaitResourceAck(ctx, m.bus, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: newStore,
			Meta:     entry.Meta,
		},
	}, "memory store update"); err != nil {
		m.stores[entry.ID] = store
		return err
	}

	// ServiceRegister is the supervisor's handover verb: it retires the
	// controller holding the superseded store and starts the replacement.
	// ServiceUpdate is the supervisor's own outbound state notification and has
	// no inbound handler.
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: newStore,
			Config:  cfg.Lifecycle,
		},
	})

	m.log.Info("updated memory store",
		zap.String("id", entry.ID.String()),
		zap.Int("max_size", cfg.MaxSize))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != memstore.KV {
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	store, exists := m.stores[entry.ID]
	if !exists {
		return storesvc.NewStoreNotFoundError(entry.ID.String())
	}

	// Get configuration for stop timeout
	cfg := store.config

	// Stop the store (but don't wait for it to complete)
	stopCtx, cancel := context.WithTimeout(ctx, cfg.Lifecycle.StopTimeout)
	defer cancel()

	if err := store.Stop(stopCtx); err != nil {
		m.log.Warn("failed to stop store cleanly during deletion",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
	}

	// Unregister from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})

	// Unregister resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	delete(m.stores, entry.ID)

	m.log.Info("deleted memory store",
		zap.String("id", entry.ID.String()))

	return nil
}
