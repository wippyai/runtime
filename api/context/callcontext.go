package context

import (
	"context"
	"sync"
)

// CallContext stores execution-level key-value pairs.
// Can reference a parent CallContext (for metadata/tracing, not value inheritance).
type CallContext interface {
	// Get retrieves a value by key from this context only.
	// Does NOT walk up parent chain automatically.
	Get(key any) any

	// Set stores a value by key in this context.
	Set(key any, value any)

	// Iterate calls fn for each key-value pair in this context only.
	Iterate(fn func(key any, value any))

	// Parent returns the parent CallContext, or nil if none.
	// Use this for manual inspection/tracing.
	Parent() CallContext

	// WithParent returns a new CallContext with specified parent.
	WithParent(parent CallContext) CallContext
}

// callContext is the concrete implementation of CallContext.
type callContext struct {
	mu     sync.RWMutex
	values map[any]any
	parent CallContext
}

// NewCallContext creates a new CallContext instance.
func NewCallContext() CallContext {
	return &callContext{
		values: make(map[any]any),
	}
}

func (c *callContext) Get(key any) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.values[key]
}

func (c *callContext) Set(key any, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

func (c *callContext) Iterate(fn func(key any, value any)) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k, v := range c.values {
		fn(k, v)
	}
}

func (c *callContext) Parent() CallContext {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.parent
}

func (c *callContext) WithParent(parent CallContext) CallContext {
	return &callContext{
		values: c.values,
		parent: parent,
	}
}

// callContextKey is the context key for storing CallContext.
var callContextKey = &Key{Name: "context.call"}

// emptyCallContext is a singleton for default/empty CallContext.
var emptyCallContext = NewCallContext()

// WithCallContext attaches CallContext to the provided context.
func WithCallContext(ctx context.Context, cc CallContext) context.Context {
	return context.WithValue(ctx, callContextKey, cc)
}

// CallFromContext extracts CallContext from context.
// Returns empty CallContext if not present.
func CallFromContext(ctx context.Context) CallContext {
	if cc, ok := ctx.Value(callContextKey).(CallContext); ok {
		return cc
	}
	return emptyCallContext
}

// WithoutCallContext removes CallContext for isolation (like DetachUnitOfWork).
// AppContext is still inherited from parent context.
func WithoutCallContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, callContextKey, nil)
}

// CopyCallContext creates new CallContext with same values.
// Parent is NOT copied - new context is independent.
func CopyCallContext(from CallContext) CallContext {
	to := NewCallContext()
	from.Iterate(func(key any, value any) {
		to.Set(key, value)
	})
	return to
}
