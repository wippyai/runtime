// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"sync"
	"sync/atomic"
)

// queueMutex keeps ordinary queue operations on the standard mutex path.
// Context senders subscribe only when contended; unlock wakes them without
// polling, a helper goroutine, or treating mutex contention as queue capacity.
type queueMutex struct {
	waiting atomic.Pointer[queueWait]
	mu      sync.Mutex
}
type queueWait struct{ changed chan struct{} }

func (m *queueMutex) Lock() { m.mu.Lock() }
func (m *queueMutex) Unlock() {
	m.mu.Unlock()
	if waiter := m.waiting.Load(); waiter != nil && m.waiting.CompareAndSwap(waiter, nil) {
		close(waiter.changed)
	}
}
func (m *queueMutex) subscribe() <-chan struct{} {
	for {
		if waiter := m.waiting.Load(); waiter != nil {
			return waiter.changed
		}
		waiter := &queueWait{changed: make(chan struct{})}
		if m.waiting.CompareAndSwap(nil, waiter) {
			return waiter.changed
		}
	}
}
func (m *queueMutex) LockContext(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.mu.TryLock() {
			break
		}
		changed := m.subscribe()
		if m.mu.TryLock() {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
	if err := ctx.Err(); err != nil {
		m.Unlock()
		return err
	}
	return nil
}
