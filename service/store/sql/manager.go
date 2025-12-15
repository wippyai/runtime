package sql

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqlstore "github.com/wippyai/runtime/api/service/store/sql"
	storeapi "github.com/wippyai/runtime/api/store"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

// Manager handles SQL store lifecycle and resource provisioning
type Manager struct {
	log    *zap.Logger
	dtt    payload.Transcoder
	bus    event.Bus
	mu     sync.RWMutex
	stores map[registry.ID]*Store
}

// NewManager creates a new SQL store manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:    log,
		dtt:    dtt,
		bus:    bus,
		stores: make(map[registry.ID]*Store),
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqlstore.KV {
		return storeapi.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.stores[entry.ID]; exists {
		return storeapi.NewStoreAlreadyExistsError(entry.ID.String())
	}

	// Decode and initialize configuration
	cfg, err := entryutil.DecodeEntryConfig[sqlstore.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// Create SQL store
	store := NewStore(entry.ID, cfg, m.log)
	m.stores[entry.ID] = store

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

	m.log.Info("added SQL store",
		zap.String("id", entry.ID.String()),
		zap.String("table", cfg.TableName),
		zap.String("id_column", cfg.IDColumnName),
		zap.String("payload_column", cfg.PayloadColumnName),
		zap.String("expire_column", cfg.ExpireColumnName),
	)

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqlstore.KV {
		return storeapi.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	oldStore, exists := m.stores[entry.ID]
	if !exists {
		return storeapi.NewStoreNotFoundError(entry.ID.String())
	}

	// Decode and initialize updated configuration
	cfg, err := entryutil.DecodeEntryConfig[sqlstore.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// Stop old store to clean up its goroutines
	if err := oldStore.Stop(ctx); err != nil {
		m.log.Warn("failed to stop old store during update",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
	}

	// Create new store with updated config
	newStore := NewStore(entry.ID, cfg, m.log)
	m.stores[entry.ID] = newStore

	// Update supervisor entry
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: newStore,
			Config:  cfg.Lifecycle,
		},
	})

	m.log.Info("updated SQL store",
		zap.String("id", entry.ID.String()),
		zap.String("table", cfg.TableName))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqlstore.KV {
		return storeapi.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.stores[entry.ID]
	if !exists {
		return storeapi.NewStoreNotFoundError(entry.ID.String())
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

	m.log.Info("deleted SQL store",
		zap.String("id", entry.ID.String()))

	return nil
}
