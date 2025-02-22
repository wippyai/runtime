package uow

import (
	"context"
	"sync"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// UnitOfWork manages a thread-safe collection of cleanup functions that can be
// executed in order. It is typically stored in a context and used to ensure
// proper resource cleanup across an operation's lifecycle.
type UnitOfWork struct {
	mu      sync.Mutex
	closers []func() error

	// Internal context for UoW-specific lifecycle management
	ctx    context.Context
	cancel context.CancelFunc

	// Private shared state for modules using sync.Map for thread-safety
	shared sync.Map
}

// FromContext retrieves a UnitOfWork instance from the provided context.
// Returns nil if no UnitOfWork instance is found in the context.
func FromContext(ctx context.Context) *UnitOfWork {
	if ctx == nil {
		return nil
	}

	v := ctx.Value(ctxapi.CleanupCtx)
	if v == nil {
		return nil
	}

	return v.(*UnitOfWork)
}

// WithContext creates a new context containing a UnitOfWork instance.
// If the input context already has a UnitOfWork instance, it returns
// the existing context and UnitOfWork. Otherwise, it creates a new
// UnitOfWork instance and returns it along with an updated context.
func WithContext(ctx context.Context) (context.Context, *UnitOfWork) {
	uwCtx, cancel := context.WithCancel(ctx)

	closer := &UnitOfWork{
		closers: make([]func() error, 0, 4),
		ctx:     uwCtx,
		cancel:  cancel,
	}
	return context.WithValue(ctx, ctxapi.CleanupCtx, closer), closer
}

// AddCleanup appends a cleanup function to be executed when Close is called.
// Functions are executed in reverse order (LIFO - Last In, First Out).
func (c *UnitOfWork) AddCleanup(closer func() error) {
	c.mu.Lock()
	c.closers = append(c.closers, closer)
	c.mu.Unlock()
}

// AddCleanupFunc appends a cleanup function to be executed when Close is called.
// Functions are executed in reverse order (LIFO - Last In, First Out). Does not require error.
func (c *UnitOfWork) AddCleanupFunc(closer func()) {
	c.mu.Lock()
	c.closers = append(c.closers, func() error { closer(); return nil })
	c.mu.Unlock()
}

// Close executes all cleanup functions in reverse order (LIFO) and returns the first
// error encountered, if any. AddCleanup execution, all cleanup functions
// are removed from the UnitOfWork instance.
func (c *UnitOfWork) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Cancel internal context first to signal all dependent operations
	c.cancel()

	var firstErr error
	// Execute closers in reverse order
	for i := len(c.closers) - 1; i >= 0; i-- {
		if err := c.closers[i](); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	c.closers = c.closers[:0]

	// Clear all shared state
	c.shared.Range(func(key, _ interface{}) bool {
		c.shared.Delete(key)
		return true
	})

	return firstErr
}

// Context returns the UnitOfWork's internal context.
// This context is canceled when the UnitOfWork is closed.
func (c *UnitOfWork) Context() context.Context {
	return c.ctx
}

// Done returns a channel that's closed when the UnitOfWork is closed.
func (c *UnitOfWork) Done() <-chan struct{} {
	return c.ctx.Done()
}

// Get retrieves a value from the shared state.
// Returns the value and true if the key exists, nil and false otherwise.
func (c *UnitOfWork) Get(key any) (any, bool) {
	return c.shared.Load(key)
}

// GetOrStore retrieves an existing value or stores a new one if not present.
// Returns the existing or stored value and a boolean indicating if the value was loaded.
func (c *UnitOfWork) GetOrStore(key any, value any) (any, bool) {
	return c.shared.LoadOrStore(key, value)
}

// Set stores a value in the shared state.
func (c *UnitOfWork) Set(key any, value any) {
	c.shared.Store(key, value)
}

// Delete removes a value from the shared state.
func (c *UnitOfWork) Delete(key any) {
	c.shared.Delete(key)
}

// Range calls f sequentially for each key and value in the shared state.
// If f returns false, range stops the iteration.
func (c *UnitOfWork) Range(f func(key, value any) bool) {
	c.shared.Range(f)
}

// CompareAndSwap executes a compare-and-swap operation on a value in the shared state.
// Returns true if the swap was successful.
func (c *UnitOfWork) CompareAndSwap(key any, old any, new any) bool {
	return c.shared.CompareAndSwap(key, old, new)
}

// CompareAndDelete deletes the entry for key if its value equals old.
// Returns true if the entry was deleted.
func (c *UnitOfWork) CompareAndDelete(key any, old any) bool {
	return c.shared.CompareAndDelete(key, old)
}
