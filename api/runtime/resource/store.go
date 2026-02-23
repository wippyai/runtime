// SPDX-License-Identifier: MPL-2.0

package resource

import "sync"

// Store provides a unified container for process-local resources.
// Combines Table-based handle storage with cleanup management.
// Designed for fast access from both Lua and WASM runtimes.
type Store struct {
	table    *Table
	cleanups []func() error
	mu       sync.Mutex
	closed   bool
}

var storePool = sync.Pool{
	New: func() any {
		return &Store{
			table: &Table{
				entries:  make([]entry, 0, 32),
				freeList: make([]Handle, 0, 8),
			},
			cleanups: make([]func() error, 0, 8),
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
// Returns a cancel function that prevents this cleanup from running.
func (s *Store) AddCleanup(fn func() error) func() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return func() {}
	}

	idx := len(s.cleanups)
	s.cleanups = append(s.cleanups, fn)

	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if idx < len(s.cleanups) {
			s.cleanups[idx] = nil
		}
	}
}

// Close runs all cleanup functions in LIFO order and returns store to pool.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cleanups := s.cleanups
	s.cleanups = s.cleanups[:0]
	s.mu.Unlock()

	var firstErr error
	for i := len(cleanups) - 1; i >= 0; i-- {
		if cleanups[i] != nil {
			if err := cleanups[i](); err != nil && firstErr == nil {
				firstErr = err
			}
		}
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
