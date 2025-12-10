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

	// Close marks the frame as closed.
	Close() error
}

type frameContext struct {
	sealed atomic.Bool
	mu     sync.RWMutex
	values map[any]any
	closed bool
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

func (f *frameContext) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
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

// AcquireFrameContext gets a frame context from pool.
func AcquireFrameContext(parent context.Context) (context.Context, FrameContext) {
	fc := frameContextPool.Get().(*frameContext)
	fc.sealed.Store(false)
	fc.closed = false
	return WithFrameContext(parent, fc), fc
}

// ReleaseFrameContext closes any Closer values, clears the map, and returns to pool.
func ReleaseFrameContext(fc FrameContext) {
	f, ok := fc.(*frameContext)
	if !ok {
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
	f.sealed.Store(false)
	f.closed = false
	f.mu.Unlock()

	for _, closer := range closers {
		_ = closer.Close()
	}
	frameContextPool.Put(f)
}

// OpenFrameContext returns an existing unsealed frame, or creates a new one if needed.
// When creating a new frame from a sealed parent, automatically copies all keys marked with Inherit: true.
func OpenFrameContext(ctx context.Context) (context.Context, FrameContext) {
	fc := FrameFromContext(ctx)
	if fc == nil || fc.IsSealed() {
		newCtx, newFC := newFrameContext(ctx)
		if fc != nil && fc.IsSealed() {
			fc.Iterate(func(key any, value any) {
				if ctxKey, ok := key.(*Key); ok && ctxKey.Inherit {
					if cloner, ok := value.(Cloner); ok {
						_ = newFC.Set(key, cloner.Clone())
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
func OpenFrameContextOn(targetCtx context.Context, parentCtx context.Context) (context.Context, FrameContext) {
	parentFC := FrameFromContext(parentCtx)
	newCtx, newFC := newFrameContext(targetCtx)

	if parentFC != nil {
		inheritCount := 0
		parentFC.Iterate(func(key any, value any) {
			if ctxKey, ok := key.(*Key); ok {
				if ctxKey.Inherit {
					inheritCount++
					if cloner, ok := value.(Cloner); ok {
						_ = newFC.Set(key, cloner.Clone())
					} else {
						_ = newFC.Set(key, value)
					}
				}
			}
		})
	}
	return newCtx, newFC
}

func newFrameContext(parent context.Context) (context.Context, FrameContext) {
	fc := frameContextPool.Get().(*frameContext)
	fc.sealed.Store(false)
	fc.closed = false
	return WithFrameContext(parent, fc), fc
}
