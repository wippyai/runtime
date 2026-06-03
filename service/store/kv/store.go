// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/store"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/api/supervisor"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

var (
	_ store.Store        = (*Store)(nil)
	_ store.Scanner      = (*Store)(nil)
	_ store.Atomic       = (*Store)(nil)
	_ resource.Provider  = (*Store)(nil)
	_ supervisor.Service = (*Store)(nil)
)

// Store adapts a shared kv engine to api/store.Store, scoped to a namespace.
// The engine lifecycle is node-level (owned by boot); this wrapper's Start/Stop
// are lifecycle no-ops so the supervisor can manage the registry entry.
type Store struct {
	engine     kvapi.Engine
	dtt        payload.Transcoder
	log        *zap.Logger
	statusChan chan any
	id         registry.ID
	namespace  string
	mu         sync.Mutex
	closed     bool
}

// NewStore builds a namespaced store over the shared engine.
func NewStore(id registry.ID, namespace string, engine kvapi.Engine, dtt payload.Transcoder, log *zap.Logger) *Store {
	if log == nil {
		log = zap.NewNop()
	}
	return &Store{
		engine:     engine,
		dtt:        dtt,
		log:        log.With(zap.String("component", "store.kv"), zap.String("id", id.String())),
		namespace:  namespace,
		id:         id,
		statusChan: make(chan any, 1),
	}
}

// Start implements supervisor.Service.
func (s *Store) Start(_ context.Context) (<-chan any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = false
	select {
	case s.statusChan <- "store.kv started":
	default:
	}
	return s.statusChan, nil
}

// Stop implements supervisor.Service.
func (s *Store) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func mapNotFound(err error) error {
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return store.ErrKeyNotFound
	}
	return err
}

// Get implements store.Store.
func (s *Store) Get(_ context.Context, key registry.ID) (payload.Payload, error) {
	ent, err := s.engine.Get(physicalKey(s.namespace, key))
	if err != nil {
		return nil, mapNotFound(err)
	}
	return decodeValue(ent.Value), nil
}

// Set implements store.Store. A non-zero TTL binds the key to a fresh lease.
func (s *Store) Set(ctx context.Context, entry store.Entry) error {
	b, err := encodeValue(s.dtt, entry.Value)
	if err != nil {
		return err
	}
	phys := physicalKey(s.namespace, entry.Key)
	if entry.TTL > 0 {
		lease, err := s.engine.GrantLease(ctx, entry.TTL)
		if err != nil {
			return err
		}
		_, err = s.engine.SetWithLease(phys, b, lease.ID())
		return err
	}
	_, err = s.engine.Set(phys, b)
	return err
}

// Delete implements store.Store.
func (s *Store) Delete(_ context.Context, key registry.ID) error {
	return mapNotFound(s.engine.Delete(physicalKey(s.namespace, key)))
}

// Has implements store.Store.
func (s *Store) Has(_ context.Context, key registry.ID) (bool, error) {
	_, err := s.engine.Get(physicalKey(s.namespace, key))
	if err != nil {
		if errors.Is(err, kvapi.ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Scan implements store.Scanner, confined to the store's namespace.
func (s *Store) Scan(_ context.Context, opts store.ScanOptions, fn func(store.Entry) bool) error {
	count := 0
	return s.engine.Scan(physicalPrefix(s.namespace, opts.Prefix), func(e kvapi.Entry) bool {
		key, ok := logicalKey(s.namespace, e.Key)
		if !ok {
			return true
		}
		if opts.After != "" && key.String() <= opts.After {
			return true
		}
		if opts.Limit > 0 && count >= opts.Limit {
			return false
		}
		count++
		return fn(store.Entry{Key: key, Value: decodeValue(e.Value)})
	})
}

// GetVersioned implements store.Atomic.
func (s *Store) GetVersioned(_ context.Context, key registry.ID) (store.VersionedEntry, error) {
	ent, err := s.engine.Get(physicalKey(s.namespace, key))
	if err != nil {
		return store.VersionedEntry{}, mapNotFound(err)
	}
	return store.VersionedEntry{
		Entry:   store.Entry{Key: key, Value: decodeValue(ent.Value)},
		Version: store.Version(ent.Version),
	}, nil
}

// CompareAndSwap implements store.Atomic.
func (s *Store) CompareAndSwap(_ context.Context, key registry.ID, expected store.Version, entry store.Entry) (bool, error) {
	b, err := encodeValue(s.dtt, entry.Value)
	if err != nil {
		return false, err
	}
	_, ok, err := s.engine.CompareAndSwap(physicalKey(s.namespace, key), uint64(expected), b)
	return ok, err
}

// SetIfAbsent implements store.Atomic.
func (s *Store) SetIfAbsent(ctx context.Context, entry store.Entry) (bool, error) {
	b, err := encodeValue(s.dtt, entry.Value)
	if err != nil {
		return false, err
	}
	phys := physicalKey(s.namespace, entry.Key)
	if entry.TTL > 0 {
		lease, err := s.engine.GrantLease(ctx, entry.TTL)
		if err != nil {
			return false, err
		}
		_, ok, err := s.engine.SetIfAbsentWithLease(phys, b, lease.ID())
		return ok, err
	}
	_, ok, err := s.engine.SetIfAbsent(phys, b)
	return ok, err
}

// Acquire implements resource.Provider.
func (s *Store) Acquire(_ context.Context, _ registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	if mode != resource.ModeNormal {
		return nil, systemresource.ErrLocked
	}
	return &storeResource{store: s}, nil
}

type storeResource struct {
	store  *Store
	mu     sync.Mutex
	closed bool
}

func (r *storeResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, resource.ErrReleased
	}
	return store.Store(r.store), nil
}

func (r *storeResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}
