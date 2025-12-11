package context

import (
	"context"
	"sync"
	"sync/atomic"
)

var frameContextKey = &Key{Name: "context.frame"}

// FrameContext stores execution-level key-value pairs.
// Values are mutable until the frame is sealed.
type FrameContext interface {
	// Get retrieves a value by key.
	Get(key any) (any, bool)

	// Set stores a value by key. Returns error if sealed.
	Set(key any, value any) error

	// SetMultiple stores multiple key-value pairs. Returns error if sealed.
	SetMultiple(pairs ...Pair) error

	// Has checks if a key exists in this context.
	Has(key any) bool

	// Iterate calls fn for each key-value pair.
	Iterate(fn func(key any, value any))

	// Seal marks this frame as immutable.
	Seal()

	// IsSealed returns true if this frame is sealed.
	IsSealed() bool

	// IncRef increments reference count. Returns Closer to decrement when done.
	// Call before spawning async work, defer the returned Closer in the goroutine.
	IncRef() Closer

	// Close decrements refcount and releases frame when zero.
	Close() error
}

type frameContext struct {
	sealed   atomic.Bool
	refcount atomic.Int32
	mu       sync.RWMutex
	values   map[any]any
	parent   *frameContext
	closed   bool
}

var frameContextPool = sync.Pool{
	New: func() any {
		return &frameContext{values: make(map[any]any, 8)}
	},
}

func (f *frameContext) Get(key any) (any, bool) {
	if f.sealed.Load() {
		val, exists := f.values[key]
		return val, exists
	}
	f.mu.RLock()
	val, exists := f.values[key]
	f.mu.RUnlock()
	return val, exists
}

func (f *frameContext) Set(key any, value any) error {
	if f.sealed.Load() {
		return NewFrameSealedError(key)
	}
	f.mu.Lock()
	if f.sealed.Load() {
		f.mu.Unlock()
		return NewFrameSealedError(key)
	}
	f.values[key] = value
	f.mu.Unlock()
	return nil
}

func (f *frameContext) SetMultiple(pairs ...Pair) error {
	if f.sealed.Load() {
		return ErrFrameSealed
	}
	f.mu.Lock()
	if f.sealed.Load() {
		f.mu.Unlock()
		return ErrFrameSealed
	}
	for _, p := range pairs {
		f.values[p.Key] = p.Value
	}
	f.mu.Unlock()
	return nil
}

func (f *frameContext) Has(key any) bool {
	if f.sealed.Load() {
		_, exists := f.values[key]
		return exists
	}
	f.mu.RLock()
	_, exists := f.values[key]
	f.mu.RUnlock()
	return exists
}

func (f *frameContext) Iterate(fn func(key any, value any)) {
	if f.sealed.Load() {
		for k, v := range f.values {
			fn(k, v)
		}
		return
	}
	f.mu.RLock()
	for k, v := range f.values {
		fn(k, v)
	}
	f.mu.RUnlock()
}

func (f *frameContext) Seal() {
	f.mu.Lock()
	f.sealed.Store(true)
	f.mu.Unlock()
}

func (f *frameContext) IsSealed() bool {
	return f.sealed.Load()
}

func (f *frameContext) IncRef() Closer {
	f.refcount.Add(1)
	return CloserFunc(func() error {
		releaseFrame(f)
		return nil
	})
}

func (f *frameContext) Close() error {
	releaseFrame(f)
	return nil
}

// WithFrameContext attaches FrameContext to the provided context.
func WithFrameContext(ctx context.Context, fc FrameContext) context.Context {
	return context.WithValue(ctx, frameContextKey, fc)
}

// FrameFromContext extracts FrameContext from context.
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

// AcquireFrameContext is deprecated. Use OpenFrameContext instead.
// This function does not inherit values from sealed parent frames.
// Deprecated: Use OpenFrameContext which properly inherits from sealed parents.
func AcquireFrameContext(parent context.Context) (context.Context, FrameContext) {
	return OpenFrameContext(parent)
}

// ReleaseFrameContext decrements refcount and triggers chain collapse when zero.
// Only pools the frame when all references (including children) are released.
func ReleaseFrameContext(fc FrameContext) {
	f, ok := fc.(*frameContext)
	if !ok {
		return
	}
	releaseFrame(f)
}

func releaseFrame(f *frameContext) {
	newCount := f.refcount.Add(-1)
	if newCount > 0 {
		return
	}

	f.mu.Lock()
	var closers []Closer
	for _, v := range f.values {
		if closer, ok := v.(Closer); ok {
			closers = append(closers, closer)
		}
	}
	clear(f.values)
	parent := f.parent
	f.parent = nil
	f.sealed.Store(false)
	f.closed = false
	f.mu.Unlock()

	for _, closer := range closers {
		_ = closer.Close()
	}

	if parent != nil {
		releaseFrame(parent)
	}

	frameContextPool.Put(f)
}

// OpenFrameContext returns an existing unsealed frame, or creates a new one if needed.
// When creating a new frame from a sealed parent, automatically copies all keys marked with Inherit: true.
// Uses reference counting to ensure parent frame stays alive while child iterates.
func OpenFrameContext(ctx context.Context) (context.Context, FrameContext) {
	fc := FrameFromContext(ctx)
	if fc == nil || fc.IsSealed() {
		parentFC, _ := fc.(*frameContext)
		newCtx, newFC := forkFrameContext(ctx, parentFC)
		return newCtx, newFC
	}
	return ctx, fc
}

// OpenFrameContextOn creates a new frame on targetCtx, inheriting from parentCtx.
// Uses reference counting to ensure parent frame stays alive while child iterates.
func OpenFrameContextOn(targetCtx context.Context, parentCtx context.Context) (context.Context, FrameContext) {
	parentFC, _ := FrameFromContext(parentCtx).(*frameContext)
	return forkFrameContext(targetCtx, parentFC)
}

func newFrameContext(parent context.Context) (context.Context, FrameContext) {
	fc := frameContextPool.Get().(*frameContext)
	fc.sealed.Store(false)
	fc.refcount.Store(1)
	fc.parent = nil
	fc.closed = false
	return WithFrameContext(parent, fc), fc
}

// forkFrameContext creates a new child frame from a parent frame.
// Increments parent refcount to keep parent alive until child releases.
// Copies inheritable values under parent's lock to prevent races.
func forkFrameContext(ctx context.Context, parent *frameContext) (context.Context, *frameContext) {
	if parent != nil {
		parent.refcount.Add(1)
	}

	fc := frameContextPool.Get().(*frameContext)
	fc.sealed.Store(false)
	fc.refcount.Store(1)
	fc.parent = parent
	fc.closed = false

	if parent != nil {
		parent.mu.RLock()
		for k, v := range parent.values {
			if ctxKey, ok := k.(*Key); ok && ctxKey.Inherit {
				if cloner, ok := v.(Cloner); ok {
					fc.values[k] = cloner.Clone()
				} else {
					fc.values[k] = v
				}
			}
		}
		parent.mu.RUnlock()
	}

	return WithFrameContext(ctx, fc), fc
}
