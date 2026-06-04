// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	kvcfg "github.com/wippyai/runtime/api/service/store/kv"
	"github.com/wippyai/runtime/api/store"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	storesvc "github.com/wippyai/runtime/service/store"
	"go.uber.org/zap"
)

// RaftManager is the registry.EntryListener for store.kv.raft entries. Every
// entry becomes a namespaced view over the single shared raft engine.
type RaftManager struct {
	engine kvapi.Engine
	dtt    payload.Transcoder
	bus    event.Bus
	log    *zap.Logger
	stores map[registry.ID]*Store
	mu     sync.RWMutex
}

// NewRaftManager builds a manager bound to the shared engine. A nil engine
// means raft is unavailable; Add then reports a clear error.
func NewRaftManager(engine kvapi.Engine, bus event.Bus, dtt payload.Transcoder, log *zap.Logger) *RaftManager {
	if log == nil {
		log = zap.NewNop()
	}
	return &RaftManager{
		engine: engine,
		dtt:    dtt,
		bus:    bus,
		log:    log,
		stores: make(map[registry.ID]*Store),
	}
}

// Add implements registry.EntryListener.
func (m *RaftManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != kvcfg.KindRaft {
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}
	if m.engine == nil {
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.stores[entry.ID]; exists {
		return storesvc.NewStoreAlreadyExistsError(entry.ID.String())
	}

	cfg, err := entryutil.DecodeEntryConfig[kvcfg.RaftConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	st := NewStoreWithInfo(entry.ID, cfg.Namespace, m.engine, m.dtt, m.log, store.Info{
		Backend:        store.BackendKVRaft,
		Consistency:    store.ConsistencyLinearizable,
		Durable:        true,
		List:           true,
		Versioned:      true,
		ConditionalPut: true,
		TTL:            true,
	})
	m.stores[entry.ID] = st

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data:   &supervisor.Entry{Service: st, Config: cfg.Lifecycle},
	})
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data:   resource.Entry{ID: entry.ID, Provider: st, Meta: entry.Meta},
	})

	m.log.Info("added store.kv.raft",
		zap.String("id", entry.ID.String()), zap.String("namespace", cfg.Namespace))
	return nil
}

// Update implements registry.EntryListener. Namespace/lifecycle changes recreate
// the store view; the underlying replicated data is untouched.
func (m *RaftManager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != kvcfg.KindRaft {
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.stores[entry.ID]; !exists {
		return storesvc.NewStoreNotFoundError(entry.ID.String())
	}
	cfg, err := entryutil.DecodeEntryConfig[kvcfg.RaftConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}
	st := NewStoreWithInfo(entry.ID, cfg.Namespace, m.engine, m.dtt, m.log, store.Info{
		Backend:        store.BackendKVRaft,
		Consistency:    store.ConsistencyLinearizable,
		Durable:        true,
		List:           true,
		Versioned:      true,
		ConditionalPut: true,
		TTL:            true,
	})
	m.stores[entry.ID] = st
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   entry.ID.String(),
		Data:   &supervisor.Entry{Service: st, Config: cfg.Lifecycle},
	})
	m.log.Info("updated store.kv.raft", zap.String("id", entry.ID.String()))
	return nil
}

// Delete implements registry.EntryListener.
func (m *RaftManager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != kvcfg.KindRaft {
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.stores[entry.ID]; !exists {
		return storesvc.NewStoreNotFoundError(entry.ID.String())
	}
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})
	delete(m.stores, entry.ID)
	m.log.Info("deleted store.kv.raft", zap.String("id", entry.ID.String()))
	return nil
}
