// SPDX-License-Identifier: MPL-2.0

package resource

import "sync"

// cleanupNode is an element of the store's intrusive doubly-linked cleanup
// list. The list is kept bounded by the number of live (uncancelled) cleanups
// rather than by the total number ever registered.
type cleanupNode struct {
	fn         func() error
	prev, next *cleanupNode
}

// Store provides a unified container for process-local resources.
// Combines Table-based handle storage with cleanup management.
// Designed for fast access from both Lua and WASM runtimes.
type Store struct {
	table *Table
	// head and tail bound a doubly-linked list of live cleanups.
	// Nodes are appended at tail; Close walks tail->head for LIFO order.
	head, tail *cleanupNode
	count      int
	mu         sync.Mutex
	closed     bool
}

var storePool = sync.Pool{
	New: func() any {
		return &Store{
			table: &Table{
				entries:  make([]entry, 0, 32),
				freeList: make([]Handle, 0, 8),
			},
		}
	},
}

// NewStore creates a new resource store from the pool.
func NewStore() *Store {
	s := storePool.Get().(*Store)
	s.closed = false
	return s
}

// Table returns the underlying resource table for handle-based access.
func (s *Store) Table() *Table {
	return s.table
}

// AddCleanup registers a cleanup function to run on Close.
// Cleanups run in LIFO order.
// Returns a cancel function that unlinks this cleanup; cancel is idempotent
// and safe to call after Close.
func (s *Store) AddCleanup(fn func() error) func() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return func() {}
	}

	node := &cleanupNode{fn: fn, prev: s.tail}
	if s.tail != nil {
		s.tail.next = node
	} else {
		s.head = node
	}
	s.tail = node
	s.count++

	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.unlink(node)
	}
}

// unlink removes a node from the list. It is idempotent: a node that is not
// currently linked (already cancelled, or the list was emptied by Close) is
// left untouched. Caller must hold s.mu.
func (s *Store) unlink(node *cleanupNode) {
	// A linked node is identified by being an endpoint or having neighbors.
	if node.prev == nil && node.next == nil && s.head != node {
		return
	}

	if node.prev != nil {
		node.prev.next = node.next
	} else {
		s.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		s.tail = node.prev
	}

	node.prev = nil
	node.next = nil
	node.fn = nil
	s.count--
}

// Close runs all live cleanup functions in LIFO order and returns store to pool.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	node := s.tail
	s.head = nil
	s.tail = nil
	s.count = 0
	s.mu.Unlock()

	var firstErr error
	for n := node; n != nil; {
		fn := n.fn
		next := n.prev
		n.prev = nil
		n.next = nil
		n.fn = nil
		if fn != nil {
			if err := fn(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		n = next
	}

	s.table.Reset()
	storePool.Put(s)

	return firstErr
}

// IsClosed returns true if the store has been closed.
func (s *Store) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// liveCleanups returns the number of live (uncancelled) cleanups retained.
func (s *Store) liveCleanups() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}
