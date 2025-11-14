// Package context provides application-level context management utilities.
// It includes AppContext for global key-value storage and FrameContext for
// hierarchical scoped values with parent-child relationships.
package context

import (
	"context"
	"sync"
)

// AppContext stores application-level key-value pairs.
// Uses immutable builder pattern - each With() returns the same instance for chaining.
// Keys are write-once: setting the same key twice will panic.
type AppContext interface {
	// Get retrieves a value by key. Returns nil if not found.
	Get(key any) any

	// With stores a value by key and returns this AppContext for chaining.
	// Panics if the key is already set (write-once enforcement).
	With(key any, value any) AppContext

	// Update replaces an existing value by key and returns this AppContext for chaining.
	// If the key doesn't exist, it behaves like With().
	Update(key any, value any) AppContext
}

// appContext is the concrete implementation of AppContext.
type appContext struct {
	mu     sync.RWMutex
	values map[any]any
}

// NewAppContext creates a new AppContext instance.
func NewAppContext() AppContext {
	return &appContext{
		values: make(map[any]any),
	}
}

func (a *appContext) Get(key any) any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.values[key]
}

func (a *appContext) With(key any, value any) AppContext {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.values[key]; exists {
		panic("cannot overwrite AppContext key: key already set")
	}
	a.values[key] = value
	return a
}

func (a *appContext) Update(key any, value any) AppContext {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.values[key] = value
	return a
}

// appContextKey is the context key for storing AppContext.
var appContextKey = &Key{Name: "context.app"}

// WithAppContext attaches AppContext to the provided context.
// This should be called ONCE at application startup.
func WithAppContext(ctx context.Context, ac AppContext) context.Context {
	return context.WithValue(ctx, appContextKey, ac)
}

// AppFromContext extracts AppContext from context.
// Returns nil if not present.
func AppFromContext(ctx context.Context) AppContext {
	if ac, ok := ctx.Value(appContextKey).(AppContext); ok {
		return ac
	}
	return nil
}

// NewRootContext creates a new root context with an empty AppContext attached.
// This is the standard way to create contexts for both application and test code.
func NewRootContext() context.Context {
	ctx := context.Background()
	appCtx := NewAppContext()
	return WithAppContext(ctx, appCtx)
}
