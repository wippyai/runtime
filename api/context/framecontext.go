// Package context provides application-level context management utilities.
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

// Cloner is implemented by types that can create a copy of themselves.
// Used during frame inheritance to prevent shared mutable state.
type Cloner interface {
	Clone() any
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

	// Close marks the frame as closed.
	// Safe to call multiple times - subsequent calls are no-ops.
	// Note: Resource cleanup should be handled via resource.Store.AddCleanup().
	Close() error
}

// frameContext is the concrete implementation of FrameContext.
type frameContext struct {
	mu     sync.RWMutex
	values map[any]any
	parent FrameContext
	sealed bool
	closed bool
}

// frameContextPool for reusing frame contexts to reduce allocations.
var frameContextPool = sync.Pool{
	New: func() any {
		return &frameContext{
			values: make(map[any]any, 4),
		}
	},
}

// AcquireFrameContext gets a frame context from the pool and wraps it with the parent context.
// Call ReleaseFrameContext when done to return it to the pool.
func AcquireFrameContext(parent context.Context) (context.Context, FrameContext) {
	fc := frameContextPool.Get().(*frameContext)
	fc.sealed = false
	fc.closed = false
	fc.parent = nil
	if parentFC := FrameFromContext(parent); parentFC != nil {
		fc.parent = parentFC
	}
	return WithFrameContext(parent, fc), fc
}

// ReleaseFrameContext returns a frame context to the pool after clearing its values.
func ReleaseFrameContext(fc FrameContext) {
	if f, ok := fc.(*frameContext); ok {
		f.mu.Lock()
		for k := range f.values {
			delete(f.values, k)
		}
		f.parent = nil
		f.sealed = false
		f.closed = false
		f.mu.Unlock()
		frameContextPool.Put(f)
	}
}

// newFrameContext creates a new FrameContext with optional parent.
// No values are inherited from parent by default.
// Parent link is kept for debugging/tracing only.
// Use OpenFrameContext instead - it handles automatic inheritance of keys marked with Inherit: true.
func newFrameContext(parent context.Context) (context.Context, FrameContext) {
	fc := &frameContext{
		values: make(map[any]any, 4),
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
					// Clone value if it implements Cloner to prevent shared mutable state
					if cloner, ok := value.(Cloner); ok {
						cloned := cloner.Clone()
						_ = newFC.Set(key, cloned)
					} else {
						_ = newFC.Set(key, value)
					}
				}
			})
		}

		return newCtx, newFC
	}

	return ctx, fc
}

// OpenFrameContextOn creates a new frame on targetCtx, inheriting from parentCtx.
// This is used when you need to fork a context chain - creating a new frame on a different
// context (like Host's context) while inheriting actor/scope from the calling context.
// The parent frame from parentCtx is linked and inheritable keys are copied.
func OpenFrameContextOn(targetCtx context.Context, parentCtx context.Context) (context.Context, FrameContext) {
	parentFC := FrameFromContext(parentCtx)

	// Create new frame on target context with parent link
	newCtx, newFC := newFrameContext(targetCtx)

	// If there's a parent frame, copy all inheritable keys
	if parentFC != nil {
		parentFC.Iterate(func(key any, value any) {
			// Check if key is a *Key with Inherit=true
			if ctxKey, ok := key.(*Key); ok && ctxKey.Inherit {
				// Clone value if it implements Cloner to prevent shared mutable state
				if cloner, ok := value.(Cloner); ok {
					cloned := cloner.Clone()
					_ = newFC.Set(key, cloned)
				} else {
					_ = newFC.Set(key, value)
				}
			}
		})
	}

	return newCtx, newFC
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

func (f *frameContext) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
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
