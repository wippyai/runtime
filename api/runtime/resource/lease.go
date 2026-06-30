// SPDX-License-Identifier: MPL-2.0

package resource

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/wippyai/runtime/api/registry"
	apiresource "github.com/wippyai/runtime/api/resource"
)

// TypeRegistryLease is the resource table type ID for registry-backed leases.
const TypeRegistryLease uint32 = 0x01

type registryLeaseKey struct {
	id   string
	mode apiresource.AccessMode
}

type registryLeaseEntry struct {
	key   registryLeaseKey
	res   apiresource.Resource[any]
	value any
}

func (e *registryLeaseEntry) Drop() {
	if e.res != nil {
		e.res.Release()
		e.res = nil
	}
	e.value = nil
}

// Lease is a logical handle to a process-local registry resource lease.
// Multiple Lease values can point at the same underlying table entry; releasing
// one lease only returns that logical borrow.
type Lease struct {
	store    *Store
	key      registryLeaseKey
	handle   Handle
	released atomic.Bool
}

var _ apiresource.Resource[any] = (*Lease)(nil)

// AcquireRegistryResource acquires a registry resource using the process-local
// resource store when available. For normal-mode resources this deduplicates
// repeated acquisitions inside a process while still returning independent
// logical handles to callers.
func AcquireRegistryResource(ctx context.Context, reg apiresource.Registry, id registry.ID, mode apiresource.AccessMode) (apiresource.Resource[any], any, error) {
	if s := GetStore(ctx); s != nil && mode == apiresource.ModeNormal {
		return s.acquireRegistryResource(ctx, reg, id, mode)
	}
	return acquireDirectRegistryResource(ctx, reg, id, mode)
}

func acquireDirectRegistryResource(ctx context.Context, reg apiresource.Registry, id registry.ID, mode apiresource.AccessMode) (apiresource.Resource[any], any, error) {
	if reg == nil {
		return nil, nil, apiresource.ErrNotFound
	}

	res, err := reg.Acquire(ctx, id, mode)
	if err != nil {
		return nil, nil, err
	}

	value, err := res.Get()
	if err != nil {
		res.Release()
		return nil, nil, err
	}

	return res, value, nil
}

func (s *Store) acquireRegistryResource(ctx context.Context, reg apiresource.Registry, id registry.ID, mode apiresource.AccessMode) (*Lease, any, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	key := registryLeaseKey{id: id.String(), mode: mode}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, nil, apiresource.ErrReleased
	}
	if s.leases == nil {
		s.leases = make(map[registryLeaseKey]Handle, 4)
	}

	if handle, ok := s.leases[key]; ok {
		value, borrowed := s.borrowRegistryLeaseLocked(key, handle)
		if borrowed {
			s.mu.Unlock()
			return &Lease{store: s, key: key, handle: handle}, value, nil
		}
		delete(s.leases, key)
	}

	res, value, err := acquireDirectRegistryResource(ctx, reg, id, mode)
	if err != nil {
		s.mu.Unlock()
		return nil, nil, err
	}

	entry := &registryLeaseEntry{key: key, res: res, value: value}
	handle := s.table.Insert(TypeRegistryLease, entry)
	if handle == 0 {
		entry.Drop()
		s.mu.Unlock()
		return nil, nil, apiresource.ErrReleased
	}
	if !s.table.Borrow(handle) {
		_, _ = s.table.Remove(handle)
		s.mu.Unlock()
		return nil, nil, apiresource.ErrReleased
	}

	s.leases[key] = handle
	s.mu.Unlock()

	return &Lease{store: s, key: key, handle: handle}, value, nil
}

func (s *Store) borrowRegistryLeaseLocked(key registryLeaseKey, handle Handle) (any, bool) {
	raw, ok := s.table.GetTyped(handle, TypeRegistryLease)
	if !ok {
		return nil, false
	}
	entry, ok := raw.(*registryLeaseEntry)
	if !ok || entry.key != key {
		return nil, false
	}
	if !s.table.Borrow(handle) {
		return nil, false
	}
	return entry.value, true
}

func (s *Store) releaseRegistryLease(key registryLeaseKey, handle Handle) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	if !s.table.ReturnBorrow(handle) {
		return
	}
	if _, removed := s.table.Remove(handle); removed {
		if current, ok := s.leases[key]; ok && current == handle {
			delete(s.leases, key)
		}
	}
}

// Get returns the underlying registry resource value while this logical lease is
// still live.
func (l *Lease) Get() (any, error) {
	if l == nil || l.released.Load() || l.store == nil {
		return nil, apiresource.ErrReleased
	}

	raw, ok := l.store.table.GetTyped(l.handle, TypeRegistryLease)
	if !ok {
		return nil, apiresource.ErrReleased
	}
	entry, ok := raw.(*registryLeaseEntry)
	if !ok || entry.key != l.key {
		return nil, fmt.Errorf("invalid registry lease entry")
	}
	return entry.value, nil
}

// Release returns this logical lease. The underlying resource is released only
// after the last logical lease is released or when the process resource store
// closes.
func (l *Lease) Release() {
	if l == nil || !l.released.CompareAndSwap(false, true) || l.store == nil {
		return
	}
	l.store.releaseRegistryLease(l.key, l.handle)
}
