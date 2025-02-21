package closer

import (
	"context"
	"sync"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Closer manages a thread-safe collection of cleanup functions that can be
// executed in order. It is typically stored in a context and used to ensure
// proper resource cleanup across an operation's lifecycle.
type Closer struct {
	mu      sync.Mutex
	closers []func() error
	done    chan struct{}
}

// NewCloser creates a new Closer instance with an initial capacity
// for storing cleanup function.
func NewCloser() *Closer {
	return &Closer{
		closers: make([]func() error, 0, 4),
		done:    make(chan struct{}),
	}
}

// FromContext retrieves a Closer instance from the provided context.
// Returns nil if no Closer instance is found in the context.
func FromContext(ctx context.Context) *Closer {
	v := ctx.Value(ctxapi.CleanupCtx)
	if v == nil {
		return nil
	}
	return v.(*Closer)
}

// WithContext creates a new context containing a Closer instance.
// If the input context already has a Closer instance, it returns
// the existing context and Closer. Otherwise, it creates a new
// Closer instance and returns it along with an updated context.
func WithContext(ctx context.Context) (context.Context, *Closer) {
	// check if there is already a closer in the context
	if closer := FromContext(ctx); closer != nil {
		return ctx, closer
	}

	closer := NewCloser()
	return context.WithValue(ctx, ctxapi.CleanupCtx, closer), closer
}

// Add appends a cleanup function to be executed when Close is called.
// Functions are executed in the order they were added.
func (c *Closer) Add(closer func() error) {
	c.mu.Lock()
	c.closers = append(c.closers, closer)
	c.mu.Unlock()
}

// Close executes all cleanup functions in order and returns the first
// error encountered, if any. After execution, all cleanup functions
// are removed from the Closer instance.
func (c *Closer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstErr error
	for _, closer := range c.closers {
		if err := closer(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	c.closers = c.closers[:0]
	close(c.done)

	return firstErr
}

func (c *Closer) Done() <-chan struct{} {
	return c.done
}
