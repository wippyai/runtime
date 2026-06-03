// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"container/heap"
	"context"
	"sync"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// lease implements kvapi.Lease.
type lease struct {
	keepAlive func(context.Context) error
	revoke    func(context.Context) error
	done      chan struct{}
	id        kvapi.LeaseID
	ttl       time.Duration
	closeOnce sync.Once
}

func newLease(id kvapi.LeaseID, ttl time.Duration) *lease {
	return &lease{
		id:   id,
		ttl:  ttl,
		done: make(chan struct{}),
	}
}

func (l *lease) ID() kvapi.LeaseID     { return l.id }
func (l *lease) TTL() time.Duration    { return l.ttl }
func (l *lease) Done() <-chan struct{} { return l.done }

func (l *lease) KeepAlive(ctx context.Context) error {
	if l.keepAlive != nil {
		return l.keepAlive(ctx)
	}
	return nil
}

func (l *lease) Revoke(ctx context.Context) error {
	if l.revoke != nil {
		return l.revoke(ctx)
	}
	return nil
}

func (l *lease) close() {
	l.closeOnce.Do(func() { close(l.done) })
}

// leaseHeapEntry tracks a lease's expiry time for the min-heap.
type leaseHeapEntry struct {
	expiresAt time.Time
	id        kvapi.LeaseID
	index     int // managed by heap.Interface
}

// leaseHeap is a min-heap of lease entries ordered by expiry time.
type leaseHeap []*leaseHeapEntry

func (h leaseHeap) Len() int           { return len(h) }
func (h leaseHeap) Less(i, j int) bool { return h[i].expiresAt.Before(h[j].expiresAt) }
func (h leaseHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *leaseHeap) Push(x any) {
	entry := x.(*leaseHeapEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}

func (h *leaseHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	entry.index = -1
	*h = old[:n-1]
	return entry
}

// leaseManager tracks active leases and their expiry times.
// All methods must be called from the event loop goroutine.
type leaseManager struct {
	entries map[kvapi.LeaseID]*leaseHeapEntry
	handles map[kvapi.LeaseID]*lease
	heap    leaseHeap
}

func newLeaseManager() *leaseManager {
	return &leaseManager{
		entries: make(map[kvapi.LeaseID]*leaseHeapEntry),
		handles: make(map[kvapi.LeaseID]*lease),
	}
}

// grant creates a new lease with the given TTL.
func (m *leaseManager) grant(id kvapi.LeaseID, ttl time.Duration, now time.Time) *lease {
	entry := &leaseHeapEntry{
		id:        id,
		expiresAt: now.Add(ttl),
	}
	heap.Push(&m.heap, entry)
	m.entries[id] = entry

	handle := newLease(id, ttl)
	m.handles[id] = handle
	return handle
}

// renew extends a lease's TTL from the current time.
func (m *leaseManager) renew(id kvapi.LeaseID, now time.Time) bool {
	entry, ok := m.entries[id]
	if !ok {
		return false
	}
	handle, ok := m.handles[id]
	if !ok {
		return false
	}

	entry.expiresAt = now.Add(handle.ttl)
	heap.Fix(&m.heap, entry.index)
	return true
}

// revoke removes a lease and closes its handle.
func (m *leaseManager) revoke(id kvapi.LeaseID) bool {
	entry, ok := m.entries[id]
	if !ok {
		return false
	}

	heap.Remove(&m.heap, entry.index)
	delete(m.entries, id)

	if handle, ok := m.handles[id]; ok {
		handle.close()
		delete(m.handles, id)
	}
	return true
}

// expired returns all lease IDs that have expired as of the given time.
// Does not remove them — caller must call revoke for each.
func (m *leaseManager) expired(now time.Time) []kvapi.LeaseID {
	var result []kvapi.LeaseID
	for m.heap.Len() > 0 {
		top := m.heap[0]
		if top.expiresAt.After(now) {
			break
		}
		result = append(result, top.id)
		heap.Pop(&m.heap)
		delete(m.entries, top.id)
	}
	return result
}

// nextExpiry returns the time of the next lease expiry, or zero if none.
func (m *leaseManager) nextExpiry() (time.Time, bool) {
	if m.heap.Len() == 0 {
		return time.Time{}, false
	}
	return m.heap[0].expiresAt, true
}

// getHandle returns the lease handle for external callers.
func (m *leaseManager) getHandle(id kvapi.LeaseID) (*lease, bool) {
	h, ok := m.handles[id]
	return h, ok
}
