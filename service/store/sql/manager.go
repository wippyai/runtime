// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqlstore "github.com/wippyai/runtime/api/service/store/sql"
	"github.com/wippyai/runtime/api/supervisor"
	storesvc "github.com/wippyai/runtime/service/store"
	entryutil "github.com/wippyai/runtime/system/entry"
	"go.uber.org/zap"
)

// Manager handles SQL store lifecycle and resource provisioning
type Manager struct {
	dtt    payload.Transcoder
	bus    event.Bus
	log    *zap.Logger
	stores map[registry.ID]*Store
	mu     sync.RWMutex
}

// NewManager creates a new SQL store manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
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
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.stores[entry.ID]; exists {
		return storesvc.NewStoreAlreadyExistsError(entry.ID.String())
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
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.stores[entry.ID]; !exists {
		return storesvc.NewStoreNotFoundError(entry.ID.String())
	}

	// Decode and initialize updated configuration
	cfg, err := entryutil.DecodeEntryConfig[sqlstore.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// The bus drops sends on a cancelled context, which would leave the
	// supervisor and the resource registry holding the superseded store with
	// nothing to correct them. Fail the operation so the registry transaction
	// rolls back instead.
	if err := ctx.Err(); err != nil {
		return err
	}

	// Running store configuration cannot change in place, so the entry update
	// installs a replacement. The superseded store keeps serving acquisitions
	// until the supervisor retires it, so no acquisition lands on a stopped
	// store.
	newStore := NewStore(entry.ID, cfg, m.log)
	m.stores[entry.ID] = newStore

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

	// Point the resource registry at the replacement
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: newStore,
			Meta:     entry.Meta,
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
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.stores[entry.ID]
	if !exists {
		return storesvc.NewStoreNotFoundError(entry.ID.String())
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
