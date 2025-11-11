package memstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/service/memstore"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/internal/config"
	"go.uber.org/zap"
)

// Manager handles memory store lifecycle and resource provisioning
type Manager struct {
	log    *zap.Logger
	dtt    payload.Transcoder
	bus    event.Bus
	mu     sync.RWMutex
	stores map[registry.ID]*MemoryStore
}

// NewManager creates a new memory store manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:    log,
		dtt:    dtt,
		bus:    bus,
		stores: make(map[registry.ID]*MemoryStore),
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != memstore.KindMemoryKV {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.stores[entry.ID]; exists {
		return fmt.Errorf("store %s already exists", entry.ID)
	}

	// Decode and initialize configuration
	cfg, err := config.DecodeAndInitConfig[memstore.MemoryConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// Create memory store
	store := NewMemoryStore(entry.ID, cfg, m.log)
	m.stores[entry.ID] = store

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
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
	if entry.Kind != memstore.KindMemoryKV {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	store, exists := m.stores[entry.ID]
	if !exists {
		return fmt.Errorf("store %s not found", entry.ID)
	}

	// Decode and initialize updated configuration
	cfg, err := config.DecodeAndInitConfig[memstore.MemoryConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// We can't update running store configuration, so we need to recreate it
	// First stop the current store
	stopCtx, cancel := context.WithTimeout(ctx, cfg.Lifecycle.StopTimeout)
	defer cancel()

	if err := store.Stop(stopCtx); err != nil {
		m.log.Warn("failed to stop store cleanly during update",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
	}

	// Create new store with updated config
	newStore := NewMemoryStore(entry.ID, cfg, m.log)
	m.stores[entry.ID] = newStore

	// Update supervisor entry
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: newStore,
			Config:  cfg.Lifecycle,
		},
	})

	// Resource registration is already in place, no need to re-register

	m.log.Info("updated memory store",
		zap.String("id", entry.ID.String()),
		zap.Int("max_size", cfg.MaxSize))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != memstore.KindMemoryKV {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	store, exists := m.stores[entry.ID]
	if !exists {
		return fmt.Errorf("store %s not found", entry.ID)
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
		Kind:   supervisor.Remove,
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
