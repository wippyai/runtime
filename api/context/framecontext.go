package context

import (
	"context"
	"fmt"
	"sync"
)

// Pair represents a key-value pair for batch operations.
type Pair struct {
	Key   any
	Value any
}

// FrameContext stores execution-level key-value pairs.
// Keys can be any comparable type (*Key, string, etc.).
// Values are mutable until the frame is sealed.
// Once sealed, no more changes are allowed.
type FrameContext interface {
	// Get retrieves a value by key. Returns (value, exists).
	Get(key any) (any, bool)

	// Set stores a value by key. Returns error if sealed.
	Set(key any, value any) error

	// SetMultiple stores multiple key-value pairs. Returns error if sealed.
	// More efficient than multiple Set calls as it acquires lock only once.
	SetMultiple(pairs ...Pair) error

	// Has checks if a key exists in this context.
	Has(key any) bool

	// Iterate calls fn for each key-value pair in this context.
	Iterate(fn func(key any, value any))

	// Parent returns the parent FrameContext, or nil if none.
	// For metadata/tracing only - values are NOT inherited via Get().
	Parent() FrameContext

	// Seal marks this frame as immutable. No more Set() calls allowed.
	Seal()

	// IsSealed returns true if this frame is sealed (immutable).
	IsSealed() bool
}

// frameContext is the concrete implementation of FrameContext.
type frameContext struct {
	mu     sync.RWMutex
	values map[any]any
	parent FrameContext
	sealed bool
}

// newFrameContext creates a new FrameContext with optional parent.
// No values are inherited from parent by default.
// Parent link is kept for debugging/tracing only.
// Use OpenFrameContext instead - it handles automatic inheritance of keys marked with Inherit: true.
func newFrameContext(parent context.Context) (context.Context, FrameContext) {
	fc := &frameContext{
		values: make(map[any]any),
		sealed: false,
	}

	// Link to parent FrameContext if exists (for debugging/tracing only)
	if parentFC := FrameFromContext(parent); parentFC != nil {
		fc.parent = parentFC
	}

	return WithFrameContext(parent, fc), fc
}

// OpenFrameContext returns an existing unsealed frame, or creates a new one if needed.
// This is the recommended way to get a frame for modification.
// When creating a new frame from a sealed parent, automatically copies all keys marked with Inherit: true.
func OpenFrameContext(ctx context.Context) (context.Context, FrameContext) {
	fc := FrameFromContext(ctx)
	if fc == nil || fc.IsSealed() {
		newCtx, newFC := newFrameContext(ctx)

		// If there was a sealed parent frame, copy all inheritable keys
		if fc != nil && fc.IsSealed() {
			fc.Iterate(func(key any, value any) {
				// Check if key is a *Key with Inherit=true
				if ctxKey, ok := key.(*Key); ok && ctxKey.Inherit {
					_ = newFC.Set(key, value)
				}
			})
		}

		return newCtx, newFC
	}

	return ctx, fc
}

func (f *frameContext) Get(key any) (any, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	val, exists := f.values[key]
	return val, exists
}

func (f *frameContext) Set(key any, value any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.sealed {
		return fmt.Errorf("cannot set key in sealed frame: %v", key)
	}

	f.values[key] = value
	return nil
}

func (f *frameContext) SetMultiple(pairs ...Pair) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.sealed {
		return fmt.Errorf("cannot set keys in sealed frame")
	}

	for _, p := range pairs {
		f.values[p.Key] = p.Value
	}
	return nil
}

func (f *frameContext) Has(key any) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, exists := f.values[key]
	return exists
}

func (f *frameContext) Iterate(fn func(key any, value any)) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for k, v := range f.values {
		fn(k, v)
	}
}

func (f *frameContext) Parent() FrameContext {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.parent
}

func (f *frameContext) Seal() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sealed = true
}

func (f *frameContext) IsSealed() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.sealed
}

// frameContextKey is the context key for storing FrameContext.
var frameContextKey = &Key{Name: "context.frame"}

// WithFrameContext attaches FrameContext to the provided context.
func WithFrameContext(ctx context.Context, fc FrameContext) context.Context {
	return context.WithValue(ctx, frameContextKey, fc)
}

// FrameFromContext extracts FrameContext from context.
// Returns nil if not present.
func FrameFromContext(ctx context.Context) FrameContext {
	if fc, ok := ctx.Value(frameContextKey).(FrameContext); ok {
		return fc
	}
	return nil
}

// CallFromContext is deprecated. Use FrameFromContext instead.
func CallFromContext(ctx context.Context) FrameContext {
	return FrameFromContext(ctx)
}
