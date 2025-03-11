package engine

import (
	"container/list"
	"context"
	"sync"
	"sync/atomic"
)

//------------------------------------------------------------------------------
// Value Store Implementation
//------------------------------------------------------------------------------

// valueStore provides thread-safe storage for arbitrary values.
// It implements the ValueStore interface using sync.Map for concurrent access.
type valueStore struct {
	values sync.Map
}

// newValueStore creates a new value store instance
func newValueStore() *valueStore {
	return &valueStore{}
}

// Get retrieves a value by key.
// Returns the value and a boolean indicating whether the key was found.
func (s *valueStore) Get(key any) (any, bool) {
	return s.values.Load(key)
}

// Set stores a value with the given key.
func (s *valueStore) Set(key any, value any) {
	s.values.Store(key, value)
}

// Delete removes a value with the given key.
func (s *valueStore) Delete(key any) {
	s.values.Delete(key)
}

// GetOrStore retrieves an existing value or stores a new one.
// Returns the value (either existing or new) and a boolean indicating whether the value was loaded.
func (s *valueStore) GetOrStore(key any, value any) (any, bool) {
	return s.values.LoadOrStore(key, value)
}

// CompareAndSwap performs atomic compare-and-swap operation.
// Returns true if the swap was successful.
func (s *valueStore) CompareAndSwap(key any, old any, new any) bool {
	return s.values.CompareAndSwap(key, old, new)
}

func (s *valueStore) reset() {
	s.values = sync.Map{}
}

// Interface implementation verification
var _ ValueStore = (*valueStore)(nil)

// resourceManager handles resource lifecycle and cleanup.
// It maintains a linked list of cleanup functions that are executed in LIFO order.
type resourceManager struct {
	mu       sync.Mutex
	closers  *list.List
	closing  atomic.Bool // Flag to indicate that close is in progress
	closed   bool
	closeErr error
}

// newResourceManager creates a new resource manager
func newResourceManager() *resourceManager {
	return &resourceManager{
		closers: list.New(),
	}
}

// AddCleanup registers a function to be called on close.
// If the manager is already closed, the function is executed immediately.
// Returns a CancelFunc that can be called to remove this cleanup function.
func (r *resourceManager) AddCleanup(fn func() error) context.CancelFunc {
	// Fast path check to avoid lock if already closing
	if r.closing.Load() {
		// Execute immediately if we're already in the process of closing
		_ = fn()
		return func() {}
	}

	r.mu.Lock()

	// Double-check after acquiring lock
	if r.closed {
		r.mu.Unlock()
		_ = fn()
		return func() {}
	}

	// Add to closers list
	element := r.closers.PushBack(fn)
	r.mu.Unlock()

	once := sync.Once{}

	// Return cancel function that both executes fn and removes it from list
	// This ensures exactly-once execution
	return func() {
		once.Do(func() {
			// Only attempt if we're not in closing process
			if !r.closing.Load() {
				r.mu.Lock()
				if !r.closed {
					// Remove from list - only if still in list
					if element.Value != nil {
						r.closers.Remove(element)
						element.Value = nil // Mark as processed
						r.mu.Unlock()
						// Execute function after releasing lock to prevent deadlocks
						_ = fn()
						return
					}
				}
				r.mu.Unlock()
			}
		})
	}
}

// Close executes all cleanup functions in reverse order (LIFO).
// It returns the first encountered error, if any.
func (r *resourceManager) Close() error {
	// Fast check to avoid lock if already closing/closed
	if r.closing.Load() {
		r.mu.Lock()
		err := r.closeErr
		r.mu.Unlock()
		return err
	}

	// Mark as closing to prevent deadlocks in cancel functions
	if !r.closing.CompareAndSwap(false, true) {
		// Another goroutine is already closing
		r.mu.Lock()
		err := r.closeErr
		r.mu.Unlock()
		return err
	}

	r.mu.Lock()

	if r.closed {
		err := r.closeErr
		r.mu.Unlock()
		return err
	}

	r.closed = true

	// Create a slice of functions to execute without holding the lock
	var closers []func() error
	for e := r.closers.Back(); e != nil; e = e.Prev() {
		closers = append(closers, e.Value.(func() error))
	}

	// Clear the list to allow garbage collection
	r.closers.Init()
	r.mu.Unlock()

	// Execute closers in reverse order (LIFO)
	var firstErr error
	for _, fn := range closers {
		if err := fn(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	r.mu.Lock()
	r.closeErr = firstErr
	r.mu.Unlock()

	return firstErr
}

func (r *resourceManager) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reset to initial state
	r.closers.Init()
	r.closed = false
	r.closeErr = nil
	r.closing.Store(false)
}

// setTerminationError sets the termination error if not already set.
// This is used to record the first error that triggered termination.
func (r *resourceManager) setTerminationError(err error) {
	if err == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closeErr == nil {
		r.closeErr = err
	}
}
