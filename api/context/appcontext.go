package context

import (
	"context"
	"sync/atomic"
)

var appContextKey = &Key{Name: "context.app"}

// AppContext stores application-level key-value pairs.
// Designed for single-threaded writes during boot, then sealed for lock-free reads.
// Do NOT write from multiple goroutines before Seal() is called.
// After sealing, concurrent reads are safe (values become immutable).
type AppContext interface {
	// Get retrieves a value by key. Returns nil if not found.
	Get(key any) any

	// With stores a value by key and returns this AppContext for chaining.
	// Panics if sealed or if key already exists.
	With(key any, value any) AppContext

	// Seal marks this context as immutable. Panics on subsequent With() calls.
	Seal()

	// IsSealed returns true if this context is sealed.
	IsSealed() bool
}

type appContext struct {
	values map[any]any
	sealed atomic.Bool
}

// NewAppContext creates a new AppContext instance.
func NewAppContext() AppContext {
	return &appContext{values: make(map[any]any)}
}

func (a *appContext) Get(key any) any {
	return a.values[key]
}

func (a *appContext) With(key any, value any) AppContext {
	// Logical invariant: writes only happen during boot before Seal().
	if a.sealed.Load() {
		panic("cannot modify sealed AppContext")
	}
	// Logical invariant: keys are write-once to keep AppContext immutable after boot.
	if _, exists := a.values[key]; exists {
		panic("cannot overwrite AppContext key: key already set")
	}
	a.values[key] = value
	return a
}

func (a *appContext) Seal() {
	a.sealed.Store(true)
}

func (a *appContext) IsSealed() bool {
	return a.sealed.Load()
}

// WithAppContext attaches AppContext to the provided context.
func WithAppContext(ctx context.Context, ac AppContext) context.Context {
	return context.WithValue(ctx, appContextKey, ac)
}

// AppFromContext extracts AppContext from context.
func AppFromContext(ctx context.Context) AppContext {
	if ac, ok := ctx.Value(appContextKey).(AppContext); ok {
		return ac
	}
	return nil
}

// NewRootContext creates a new root context with an empty AppContext attached.
func NewRootContext() context.Context {
	return WithAppContext(context.Background(), NewAppContext())
}
