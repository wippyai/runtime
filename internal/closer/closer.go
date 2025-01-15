package closer

import (
	"context"
	"sync"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

type Cleanup struct {
	mu      sync.Mutex
	closers []func() error
}

func NewCleanup() *Cleanup {
	return &Cleanup{
		closers: make([]func() error, 0, 4),
	}
}

func FromContext(ctx context.Context) *Cleanup {
	v := ctx.Value(ctxapi.CleanupCtx)
	if v == nil {
		return nil
	}
	return v.(*Cleanup)
}

func WithContext(ctx context.Context) (context.Context, *Cleanup) {
	// check if there is already a cleanup in the context
	if cleanup := FromContext(ctx); cleanup != nil {
		return ctx, cleanup
	}

	cleanup := NewCleanup()
	return context.WithValue(ctx, ctxapi.CleanupCtx, cleanup), cleanup
}

func (c *Cleanup) Add(closer func() error) {
	c.mu.Lock()
	c.closers = append(c.closers, closer)
	c.mu.Unlock()
}

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
