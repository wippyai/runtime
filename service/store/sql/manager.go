package sql

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/service/sqlstore"
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
	stores map[registry.ID]*SQLStore
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
		stores: make(map[registry.ID]*SQLStore),
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqlstore.KindSQLKV {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.stores[entry.ID]; exists {
		return fmt.Errorf("store %s already exists", entry.ID)
	}

	// Decode and initialize configuration
	cfg, err := entryutil.DecodeEntryConfig[sqlstore.SQLConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// Create SQL store
	store := NewSQLStore(entry.ID, cfg, m.log)
	m.stores[entry.ID] = store

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

	m.log.Info("added SQL store",
		zap.String("id", entry.ID.String()),
		zap.String("table", cfg.TableName),
		zap.String("id", cfg.IDColumnName),
		zap.String("payload", cfg.PayloadColumnName),
		zap.String("expired", cfg.ExpireColumnName),
	)

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqlstore.KindSQLKV {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.stores[entry.ID]
	if !exists {
		return fmt.Errorf("store %s not found", entry.ID)
	}

	// Decode and initialize updated configuration
	cfg, err := entryutil.DecodeEntryConfig[sqlstore.SQLConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// Create new store with updated config
	newStore := NewSQLStore(entry.ID, cfg, m.log)
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

	m.log.Info("updated SQL store",
		zap.String("id", entry.ID.String()),
		zap.String("table", cfg.TableName))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqlstore.KindSQLKV {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.stores[entry.ID]
	if !exists {
		return fmt.Errorf("store %s not found", entry.ID)
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

	m.log.Info("deleted SQL store",
		zap.String("id", entry.ID.String()))

	return nil
}
