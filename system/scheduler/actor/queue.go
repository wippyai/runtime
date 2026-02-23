// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"sync"
	"sync/atomic"
)

// Queue is a thread-safe FIFO queue using a ring buffer.
//
// Design choices:
//   - Mutex-based for simplicity (global queue is not the hot path)
//   - Ring buffer to avoid slice append allocations
//   - Grows on demand but never shrinks (avoids churn)
//   - Atomic count for fast empty checks without locking
//
// Usage:
//   - New process submissions
//   - Cross-worker transfers after async completion
//   - Initial work distribution
//
// The global queue is NOT the hot path. Workers primarily use local deques.
// Global queue is only accessed for:
//  1. Submit() - new process enters system
//  2. Complete() from async handler - process re-enters from outside worker
//  3. findWork() fallback - when local deque is empty
type Queue struct {
	items     []*Processor
	head      int // Read position (oldest item)
	tail      int // Write position (next empty slot)
	count     int
	atomicLen atomic.Int32 // Fast lock-free length check
	mu        sync.Mutex
}

// NewQueue creates a queue with initial capacity.
// Capacity should be sized for expected concurrent processes.
func NewQueue(capacity int) *Queue {
	if capacity < 16 {
		capacity = 16
	}
	return &Queue{
		items: make([]*Processor, capacity),
	}
}

// Push adds a processor to the queue (thread-safe).
// Grows buffer if needed.
func (q *Queue) Push(p *Processor) {
	q.mu.Lock()

	if q.count == len(q.items) {
		q.grow()
	}

	q.items[q.tail] = p
	q.tail = (q.tail + 1) % len(q.items)
	q.count++
	q.atomicLen.Add(1)

	q.mu.Unlock()
}

// Pop removes and returns the oldest processor (FIFO).
// Returns nil if queue is empty.
func (q *Queue) Pop() *Processor {
	// Fast path: lock-free empty check
	if q.atomicLen.Load() == 0 {
		return nil
	}

	q.mu.Lock()

	if q.count == 0 {
		q.mu.Unlock()
		return nil
	}

	p := q.items[q.head]
	q.items[q.head] = nil // Clear reference for GC
	q.head = (q.head + 1) % len(q.items)
	q.count--
	q.atomicLen.Add(-1)

	q.mu.Unlock()
	return p
}

// PopN removes and returns up to n processors.
// Returns actual count taken. Useful for batch transfers.
func (q *Queue) PopN(dst []*Processor) int {
	// Fast path: lock-free empty check
	if q.atomicLen.Load() == 0 {
		return 0
	}

	q.mu.Lock()

	n := q.count
	if n > len(dst) {
		n = len(dst)
	}

	for i := 0; i < n; i++ {
		dst[i] = q.items[q.head]
		q.items[q.head] = nil // Clear reference for GC
		q.head = (q.head + 1) % len(q.items)
	}
	q.count -= n
	q.atomicLen.Add(int32(-n))

	q.mu.Unlock()
	return n
}

// Len returns current item count (lock-free).
func (q *Queue) Len() int {
	return int(q.atomicLen.Load())
}

// IsEmpty returns true if queue has no items (lock-free).
func (q *Queue) IsEmpty() bool {
	return q.atomicLen.Load() == 0
}

// grow doubles the buffer capacity.
// Must be called while holding the lock.
func (q *Queue) grow() {
	newCap := len(q.items) * 2
	newItems := make([]*Processor, newCap)

	// Copy items maintaining FIFO order
	for i := 0; i < q.count; i++ {
		newItems[i] = q.items[(q.head+i)%len(q.items)]
	}

	q.items = newItems
	q.head = 0
	q.tail = q.count
}
