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

	// Has checks if a key exists in this context.
	Has(key any) bool

	// Set stores a value by key. Returns error if sealed.
	Set(key any, value any) error

	// SetMultiple stores multiple key-value pairs. Returns error if sealed.
	SetMultiple(pairs ...Pair) error

	// Iterate calls fn for each key-value pair.
	Iterate(fn func(key any, value any))

	// InheritablePairs returns all key-value pairs marked with Inherit: true.
	// Used for propagating context to child processes or tasks.
	InheritablePairs() []Pair

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

// frameContext is designed for single-threaded access before sealing.
// Do NOT access from multiple goroutines until Seal() is called.
// After sealing, concurrent reads are safe (values become immutable).
type frameContext struct {
	sealed   atomic.Bool
	refcount atomic.Int32
	values   map[any]any
	parent   *frameContext
}

var frameContextPool = sync.Pool{
	New: func() any {
		return &frameContext{values: make(map[any]any, 8)}
	},
}

func (f *frameContext) Get(key any) (any, bool) {
	val, exists := f.values[key]
	return val, exists
}

func (f *frameContext) Set(key any, value any) error {
	if f.sealed.Load() {
		return NewFrameSealedError(key)
	}
	f.values[key] = value
	return nil
}

func (f *frameContext) SetMultiple(pairs ...Pair) error {
	if f.sealed.Load() {
		return ErrFrameSealed
	}
	for _, p := range pairs {
		f.values[p.Key] = p.Value
	}
	return nil
}

func (f *frameContext) Has(key any) bool {
	_, exists := f.values[key]
	return exists
}

func (f *frameContext) Iterate(fn func(key any, value any)) {
	for k, v := range f.values {
		fn(k, v)
	}
}

func (f *frameContext) InheritablePairs() []Pair {
	var pairs []Pair
	for k, v := range f.values {
		if ctxKey, ok := k.(*Key); ok && ctxKey.Inherit {
			pairs = append(pairs, Pair{Key: k, Value: v})
		}
	}
	return pairs
}

func (f *frameContext) Seal() {
	f.sealed.Store(true)
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

// PropagatedPairs returns context pairs suitable for cross-process propagation.
// It extracts all inheritable pairs from the frame context and applies the
// Propagator interface for values that need transformation before crossing process boundaries.
// Values where PropagateValue() returns nil are excluded from propagation.
func PropagatedPairs(ctx context.Context) []Pair {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return nil
	}

	inheritable := fc.InheritablePairs()
	if len(inheritable) == 0 {
		return nil
	}

	pairs := make([]Pair, 0, len(inheritable))
	for _, p := range inheritable {
		// Check if value needs transformation for cross-process propagation
		if propagator, ok := p.Value.(Propagator); ok {
			transformed := propagator.PropagateValue()
			if transformed != nil {
				pairs = append(pairs, Pair{Key: p.Key, Value: transformed})
			}
			continue
		}
		pairs = append(pairs, p)
	}
	return pairs
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

	// refcount == 0 means exclusive access, no lock needed
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
	return WithFrameContext(parent, fc), fc
}

// forkFrameContext creates a new child frame from a parent frame.
// Increments parent refcount to keep parent alive until child releases.
// Parent is always sealed when forking, so no lock needed for copying.
func forkFrameContext(ctx context.Context, parent *frameContext) (context.Context, *frameContext) {
	if parent != nil {
		parent.refcount.Add(1)
	}

	fc := frameContextPool.Get().(*frameContext)
	fc.sealed.Store(false)
	fc.refcount.Store(1)
	fc.parent = parent

	// Parent is sealed (checked in OpenFrameContext), safe to read without lock
	if parent != nil {
		for k, v := range parent.values {
			if ctxKey, ok := k.(*Key); ok && ctxKey.Inherit {
				if cloner, ok := v.(Cloner); ok {
					fc.values[k] = cloner.Clone()
				} else {
					fc.values[k] = v
				}
			}
		}
	}

	return WithFrameContext(ctx, fc), fc
}
