// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"sort"
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
	_ store.InfoProvider = (*Store)(nil)
	_ store.EntryReader  = (*Store)(nil)
	_ store.Lister       = (*Store)(nil)
	_ store.Putter       = (*Store)(nil)
	_ resource.Provider  = (*Store)(nil)
	_ supervisor.Service = (*Store)(nil)
)

type linearizableEngine interface {
	GetLinearizable(key string) (kvapi.Entry, error)
	ScanAtIndex(prefix string, fn func(kvapi.Entry) bool) (uint64, error)
}

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
	info       store.Info
	mu         sync.Mutex
	closed     bool
}

// NewStore builds a namespaced store over the shared engine.
func NewStore(id registry.ID, namespace string, engine kvapi.Engine, dtt payload.Transcoder, log *zap.Logger) *Store {
	return NewStoreWithInfo(id, namespace, engine, dtt, log, store.Info{
		Backend:        store.BackendUnknown,
		Consistency:    store.ConsistencyUnknown,
		Durable:        false,
		List:           true,
		Versioned:      true,
		ConditionalPut: true,
		TTL:            true,
	})
}

// NewStoreWithInfo builds a namespaced store and advertises stable capabilities.
func NewStoreWithInfo(id registry.ID, namespace string, engine kvapi.Engine, dtt payload.Transcoder, log *zap.Logger, info store.Info) *Store {
	if log == nil {
		log = zap.NewNop()
	}
	info.ID = id
	return &Store{
		engine:     engine,
		dtt:        dtt,
		log:        log.With(zap.String("component", "store.kv"), zap.String("id", id.String())),
		namespace:  namespace,
		id:         id,
		info:       info,
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
	if errors.Is(err, kvapi.ErrUnsupported) {
		return store.ErrUnsupported
	}
	return err
}

// StoreInfo reports the capabilities configured by the store manager.
func (s *Store) StoreInfo(_ context.Context) store.Info {
	info := s.info
	info.ID = s.id
	return info
}

// Get implements store.Store.
func (s *Store) Get(_ context.Context, key registry.ID) (payload.Payload, error) {
	ent, err := s.engine.Get(physicalKey(s.namespace, key))
	if err != nil {
		return nil, mapNotFound(err)
	}
	return decodeValue(ent.Value), nil
}

// Entry implements store.EntryReader.
func (s *Store) Entry(_ context.Context, key registry.ID) (store.VersionedEntry, error) {
	ent, err := s.getEngineEntry(key)
	if err != nil {
		return store.VersionedEntry{}, mapNotFound(err)
	}
	return versionedFromEngine(s.namespace, ent)
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
	items, err := s.collectEntries(opts.Prefix)
	if err != nil {
		return err
	}
	count := 0
	for _, item := range items {
		if opts.After != "" && item.Key.String() <= opts.After {
			continue
		}
		if opts.Limit > 0 && count >= opts.Limit {
			return nil
		}
		count++
		if !fn(item.Entry) {
			return nil
		}
	}
	return nil
}

// List implements store.Lister.
func (s *Store) List(_ context.Context, opts store.ListOptions) (store.Page, error) {
	items, err := s.collectEntries(opts.Prefix)
	if err != nil {
		return store.Page{}, err
	}
	return store.PageFromSorted(items, opts), nil
}

// GetVersioned implements store.Atomic.
func (s *Store) GetVersioned(_ context.Context, key registry.ID) (store.VersionedEntry, error) {
	ent, err := s.getEngineEntry(key)
	if err != nil {
		return store.VersionedEntry{}, mapNotFound(err)
	}
	return versionedFromEngine(s.namespace, ent)
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

// Put implements store.Putter.
func (s *Store) Put(ctx context.Context, key registry.ID, value payload.Payload, opts store.PutOptions) (store.VersionedEntry, error) {
	if opts.OnlyIfAbsent && opts.HasVersion {
		return store.VersionedEntry{}, store.ErrInvalidOptions
	}
	if opts.HasVersion && opts.Version == 0 {
		return store.VersionedEntry{}, store.ErrInvalidOptions
	}
	if opts.TTL < 0 {
		return store.VersionedEntry{}, store.ErrInvalidOptions
	}
	if opts.HasVersion && opts.TTL > 0 {
		return store.VersionedEntry{}, store.ErrInvalidOptions
	}
	if (opts.OnlyIfAbsent || opts.HasVersion) && !s.info.ConditionalPut {
		return store.VersionedEntry{}, store.ErrUnsupported
	}

	b, err := encodeValue(s.dtt, value)
	if err != nil {
		return store.VersionedEntry{}, err
	}
	phys := physicalKey(s.namespace, key)

	var version kvapi.Version
	if opts.OnlyIfAbsent {
		if opts.TTL > 0 {
			lease, err := s.engine.GrantLease(ctx, opts.TTL)
			if err != nil {
				return store.VersionedEntry{}, mapNotFound(err)
			}
			var ok bool
			version, ok, err = s.engine.SetIfAbsentWithLease(phys, b, lease.ID())
			if err != nil {
				return store.VersionedEntry{}, mapNotFound(err)
			}
			if !ok {
				_ = lease.Revoke(ctx)
				return store.VersionedEntry{}, store.ErrKeyExists
			}
		} else {
			var ok bool
			version, ok, err = s.engine.SetIfAbsent(phys, b)
			if err != nil {
				return store.VersionedEntry{}, mapNotFound(err)
			}
			if !ok {
				return store.VersionedEntry{}, store.ErrKeyExists
			}
		}
	} else if opts.HasVersion {
		var ok bool
		version, ok, err = s.engine.CompareAndSwap(phys, kvapi.Version(opts.Version), b)
		if err != nil {
			return store.VersionedEntry{}, mapNotFound(err)
		}
		if !ok {
			if version == 0 {
				return store.VersionedEntry{}, store.ErrKeyNotFound
			}
			return store.VersionedEntry{}, store.ErrVersionMismatch
		}
	} else if opts.TTL > 0 {
		lease, err := s.engine.GrantLease(ctx, opts.TTL)
		if err != nil {
			return store.VersionedEntry{}, mapNotFound(err)
		}
		version, err = s.engine.SetWithLease(phys, b, lease.ID())
		if err != nil {
			_ = lease.Revoke(ctx)
			return store.VersionedEntry{}, mapNotFound(err)
		}
	} else {
		version, err = s.engine.Set(phys, b)
		if err != nil {
			return store.VersionedEntry{}, mapNotFound(err)
		}
	}

	return store.VersionedEntry{
		Entry:   store.Entry{Key: key, Value: value, TTL: opts.TTL},
		Version: store.Version(version),
	}, nil
}

func (s *Store) getEngineEntry(key registry.ID) (kvapi.Entry, error) {
	phys := physicalKey(s.namespace, key)
	if s.info.Consistency == store.ConsistencyLinearizable {
		if engine, ok := s.engine.(linearizableEngine); ok {
			return engine.GetLinearizable(phys)
		}
	}
	return s.engine.Get(phys)
}

func (s *Store) collectEntries(prefix string) ([]store.VersionedEntry, error) {
	items := make([]store.VersionedEntry, 0)
	scan := func(e kvapi.Entry) bool {
		item, err := versionedFromEngine(s.namespace, e)
		if err != nil {
			return true
		}
		items = append(items, item)
		return true
	}

	var err error
	physPrefix := physicalPrefix(s.namespace, prefix)
	if s.info.Consistency == store.ConsistencyLinearizable {
		if engine, ok := s.engine.(linearizableEngine); ok {
			_, err = engine.ScanAtIndex(physPrefix, scan)
		} else {
			err = s.engine.Scan(physPrefix, scan)
		}
	} else {
		err = s.engine.Scan(physPrefix, scan)
	}
	if err != nil {
		return nil, mapNotFound(err)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Key.String() < items[j].Key.String()
	})
	return items, nil
}

func versionedFromEngine(namespace string, ent kvapi.Entry) (store.VersionedEntry, error) {
	key, ok := logicalKey(namespace, ent.Key)
	if !ok {
		return store.VersionedEntry{}, store.ErrInvalidKey
	}
	return store.VersionedEntry{
		Entry:   store.Entry{Key: key, Value: decodeValue(ent.Value)},
		Version: store.Version(ent.Version),
	}, nil
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
