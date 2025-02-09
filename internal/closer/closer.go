package closer

import (
	"context"
	"sync"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Cleanup manages a thread-safe collection of cleanup functions that can be
// executed in order. It is typically stored in a context and used to ensure
// proper resource cleanup across an operation's lifecycle.
type Cleanup struct {
	mu      sync.Mutex
	closers []func() error
}

// NewCleanup creates a new Cleanup instance with an initial capacity
// for storing cleanup function.
func NewCleanup() *Cleanup {
	return &Cleanup{
		closers: make([]func() error, 0, 4),
	}
}

// FromContext retrieves a Cleanup instance from the provided context.
// Returns nil if no Cleanup instance is found in the context.
func FromContext(ctx context.Context) *Cleanup {
	v := ctx.Value(ctxapi.CleanupCtx)
	if v == nil {
		return nil
	}
	return v.(*Cleanup)
}

// WithContext creates a new context containing a Cleanup instance.
// If the input context already has a Cleanup instance, it returns
// the existing context and Cleanup. Otherwise, it creates a new
// Cleanup instance and returns it along with an updated context.
func WithContext(ctx context.Context) (context.Context, *Cleanup) {
	// check if there is already a cleanup in the context
	if cleanup := FromContext(ctx); cleanup != nil {
		return ctx, cleanup
	}

	cleanup := NewCleanup()
	return context.WithValue(ctx, ctxapi.CleanupCtx, cleanup), cleanup
}

// Add appends a cleanup function to be executed when Close is called.
// Functions are executed in the order they were added.
func (c *Cleanup) Add(closer func() error) {
	c.mu.Lock()
	c.closers = append(c.closers, closer)
	c.mu.Unlock()
}

// Close executes all cleanup functions in order and returns the first
// error encountered, if any. After execution, all cleanup functions
// are removed from the Cleanup instance.
func (c *Cleanup) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstErr error
	for _, closer := range c.closers {
		if err := closer(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	c.closers = c.closers[:0]
	return firstErr
}
