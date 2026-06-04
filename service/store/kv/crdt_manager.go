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
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	storesvc "github.com/wippyai/runtime/service/store"
	systemkv "github.com/wippyai/runtime/system/kv"
	"go.uber.org/zap"
)

// CRDTManager is the registry.EntryListener for store.kv.crdt entries. Each
// entry is a namespaced view over the shared node-wide crdt engine; a durable
// entry marks its namespace for fs snapshots.
type CRDTManager struct {
	engine *systemkv.CRDTEngine
	dtt    payload.Transcoder
	bus    event.Bus
	log    *zap.Logger
	stores map[registry.ID]*Store
	mu     sync.RWMutex
}

// NewCRDTManager builds a manager bound to the shared crdt engine. A nil engine
// means gossip is unavailable; Add then reports a clear error.
func NewCRDTManager(engine *systemkv.CRDTEngine, bus event.Bus, dtt payload.Transcoder, log *zap.Logger) *CRDTManager {
	if log == nil {
		log = zap.NewNop()
	}
	return &CRDTManager{
		engine: engine,
		dtt:    dtt,
		bus:    bus,
		log:    log,
		stores: make(map[registry.ID]*Store),
	}
}

// Add implements registry.EntryListener.
func (m *CRDTManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != kvcfg.KindCRDT {
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

	cfg, err := entryutil.DecodeEntryConfig[kvcfg.CRDTConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}
	if cfg.Durable {
		m.engine.MarkDurable(cfg.Namespace)
	}

	st := NewStoreWithInfo(entry.ID, cfg.Namespace, m.engine, m.dtt, m.log, store.Info{
		Backend:        store.BackendKVCRDT,
		Consistency:    store.ConsistencyEventual,
		Durable:        cfg.Durable,
		List:           true,
		Versioned:      true,
		ConditionalPut: false,
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

	m.log.Info("added store.kv.crdt",
		zap.String("id", entry.ID.String()),
		zap.String("namespace", cfg.Namespace),
		zap.Bool("durable", cfg.Durable))
	return nil
}

// Update implements registry.EntryListener.
func (m *CRDTManager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != kvcfg.KindCRDT {
		return storesvc.NewUnsupportedKindError(entry.Kind)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.stores[entry.ID]; !exists {
		return storesvc.NewStoreNotFoundError(entry.ID.String())
	}
	cfg, err := entryutil.DecodeEntryConfig[kvcfg.CRDTConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}
	if cfg.Durable {
		m.engine.MarkDurable(cfg.Namespace)
	}
	st := NewStoreWithInfo(entry.ID, cfg.Namespace, m.engine, m.dtt, m.log, store.Info{
		Backend:        store.BackendKVCRDT,
		Consistency:    store.ConsistencyEventual,
		Durable:        cfg.Durable,
		List:           true,
		Versioned:      true,
		ConditionalPut: false,
		TTL:            true,
	})
	m.stores[entry.ID] = st
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   entry.ID.String(),
		Data:   &supervisor.Entry{Service: st, Config: cfg.Lifecycle},
	})
	m.log.Info("updated store.kv.crdt", zap.String("id", entry.ID.String()))
	return nil
}

// Delete implements registry.EntryListener.
func (m *CRDTManager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != kvcfg.KindCRDT {
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
	m.log.Info("deleted store.kv.crdt", zap.String("id", entry.ID.String()))
	return nil
}
