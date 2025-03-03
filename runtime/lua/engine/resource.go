package engine

import "sync"

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

//------------------------------------------------------------------------------
// Resource Manager Implementation
//------------------------------------------------------------------------------

// resourceManager handles resource lifecycle and cleanup.
// It maintains a list of cleanup functions that are executed in LIFO order.
type resourceManager struct {
	mu       sync.Mutex
	closers  []func() error
	closed   bool
	closeErr error
}

// newResourceManager creates a new resource manager with initial capacity for 8 closers
func newResourceManager() *resourceManager {
	return &resourceManager{
		closers: make([]func() error, 0, 8),
	}
}

// AddCleanup registers a function to be called on close.
// If the manager is already closed, the function is executed immediately.
func (r *resourceManager) AddCleanup(fn func() error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		// If already closed, execute immediately
		_ = fn()
		return
	}

	r.closers = append(r.closers, fn)
}

// Close executes all cleanup functions in reverse order (LIFO).
// It returns the first encountered error, if any.
func (r *resourceManager) Close() error {
	r.mu.Lock()

	if r.closed {
		err := r.closeErr
		r.mu.Unlock()
		return err
	}

	r.closed = true
	closers := r.closers
	r.closers = nil
	r.mu.Unlock()

	// Execute closers in reverse order (LIFO)
	var firstErr error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i](); err != nil && firstErr == nil {
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
	r.closers = make([]func() error, 0, 8)
	r.closed = false
	r.closeErr = nil
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
