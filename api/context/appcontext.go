package context

import (
	"context"
	"sync"
)

// AppContext stores application-level key-value pairs.
// Immutable after Lock() is called.
type AppContext interface {
	// Get retrieves a value by key. Returns nil if not found.
	Get(key any) any

	// Set stores a value by key. Panics if locked.
	Set(key any, value any)

	// Lock makes this AppContext immutable.
	Lock()

	// IsLocked returns true if locked.
	IsLocked() bool
}

// appContext is the concrete implementation of AppContext.
type appContext struct {
	mu     sync.RWMutex
	values map[any]any
	locked bool
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

func (a *appContext) Set(key any, value any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.locked {
		panic("cannot modify locked AppContext")
	}
	a.values[key] = value
}

func (a *appContext) Lock() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.locked = true
}

func (a *appContext) IsLocked() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.locked
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
