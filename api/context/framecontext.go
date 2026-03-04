// SPDX-License-Identifier: MPL-2.0

package context

import (
	"context"
	"runtime"
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

	// Close decrements refcount and releases frame when zero.
	Close() error
}

type frameValues map[any]any

// frameContext stores values as immutable snapshots.
// Reads stay lock-free; writes clone and swap the snapshot.
type frameContext struct {
	values atomic.Pointer[frameValues]
	parent *frameContext
	// generation increments whenever the frame lifecycle changes.
	// Context values carry the generation to reject stale frame references.
	generation atomic.Uint64
	refcount   atomic.Int32
	sealed     atomic.Bool
	writers    atomic.Int32
}

type frameContextRef struct {
	frame      *frameContext
	generation uint64
}

var frameContextPool = sync.Pool{
	New: func() any {
		f := &frameContext{}
		values := make(frameValues, 8)
		f.values.Store(&values)
		return f
	},
}

func cloneValues(src frameValues, extra int) frameValues {
	if extra < 0 {
		extra = 0
	}
	dst := make(frameValues, len(src)+extra)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (f *frameContext) valuesSnapshot() frameValues {
	valuesPtr := f.values.Load()
	if valuesPtr == nil {
		return nil
	}
	return *valuesPtr
}

func (f *frameContext) beginWrite() bool {
	if f.sealed.Load() {
		return false
	}
	f.writers.Add(1)
	if f.sealed.Load() {
		f.writers.Add(-1)
		return false
	}
	return true
}

func (f *frameContext) endWrite() {
	f.writers.Add(-1)
}

func (f *frameContext) Get(key any) (any, bool) {
	values := f.valuesSnapshot()
	val, exists := values[key]
	return val, exists
}

func (f *frameContext) Set(key any, value any) error {
	if !f.beginWrite() {
		return NewFrameSealedError(key)
	}
	defer f.endWrite()

	for {
		currentPtr := f.values.Load()
		var current frameValues
		if currentPtr != nil {
			current = *currentPtr
		}

		next := cloneValues(current, 1)
		next[key] = value
		nextPtr := &next
		if f.values.CompareAndSwap(currentPtr, nextPtr) {
			return nil
		}

		if f.sealed.Load() {
			return NewFrameSealedError(key)
		}
	}
}

func (f *frameContext) SetMultiple(pairs ...Pair) error {
	if !f.beginWrite() {
		return ErrFrameSealed
	}
	defer f.endWrite()

	for {
		currentPtr := f.values.Load()
		var current frameValues
		if currentPtr != nil {
			current = *currentPtr
		}

		next := cloneValues(current, len(pairs))
		for _, p := range pairs {
			next[p.Key] = p.Value
		}
		nextPtr := &next
		if f.values.CompareAndSwap(currentPtr, nextPtr) {
			return nil
		}

		if f.sealed.Load() {
			return ErrFrameSealed
		}
	}
}

func (f *frameContext) Has(key any) bool {
	values := f.valuesSnapshot()
	_, exists := values[key]
	return exists
}

func (f *frameContext) Iterate(fn func(key any, value any)) {
	for k, v := range f.valuesSnapshot() {
		fn(k, v)
	}
}

func (f *frameContext) InheritablePairs() []Pair {
	var pairs []Pair
	for k, v := range f.valuesSnapshot() {
		if ctxKey, ok := k.(*Key); ok && ctxKey.Inherit {
			pairs = append(pairs, Pair{Key: k, Value: v})
		}
	}
	return pairs
}

func (f *frameContext) Seal() {
	if f.sealed.Swap(true) {
		return
	}

	// Wait until in-flight writers that entered before seal complete.
	for f.writers.Load() != 0 {
		runtime.Gosched()
	}
}

func (f *frameContext) IsSealed() bool {
	return f.sealed.Load()
}

func (f *frameContext) Close() error {
	releaseFrame(f)
	return nil
}

// WithFrameContext attaches FrameContext to the provided context.
func WithFrameContext(ctx context.Context, fc FrameContext) context.Context {
	if f, ok := fc.(*frameContext); ok {
		ref := frameContextRef{
			frame:      f,
			generation: f.generation.Load(),
		}
		return context.WithValue(ctx, frameContextKey, ref)
	}
	return context.WithValue(ctx, frameContextKey, fc)
}

// FrameFromContext extracts FrameContext from context.
func FrameFromContext(ctx context.Context) FrameContext {
	switch v := ctx.Value(frameContextKey).(type) {
	case frameContextRef:
		if v.frame == nil {
			return nil
		}
		if v.generation != v.frame.generation.Load() {
			return nil
		}
		if v.frame.refcount.Load() <= 0 {
			return nil
		}
		return v.frame
	case *frameContext:
		// Backward compatibility for contexts created before frameContextRef.
		if v == nil || v.refcount.Load() <= 0 {
			return nil
		}
		return v
	case FrameContext:
		return v
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

// tryIncRef atomically increments refcount only if the frame is still alive (refcount > 0).
// Returns false if the frame has already been released, preventing use-after-pool races.
func tryIncRef(f *frameContext) bool {
	for {
		current := f.refcount.Load()
		if current <= 0 {
			return false
		}
		if f.refcount.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func releaseFrame(f *frameContext) {
	newCount := f.refcount.Add(-1)
	if newCount != 0 {
		return
	}

	// Invalidate all context-bound references before clearing/repooling.
	f.generation.Add(1)

	// refcount == 0 means exclusive access, no lock needed
	var closers []Closer
	for _, v := range f.valuesSnapshot() {
		if closer, ok := v.(Closer); ok {
			closers = append(closers, closer)
		}
	}
	values := make(frameValues, 8)
	f.values.Store(&values)
	parent := f.parent
	f.parent = nil
	f.sealed.Store(false)
	f.writers.Store(0)

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
	if parentFC != nil && !parentFC.IsSealed() {
		// Forking implies immutable parent snapshot for safe copy-on-fork.
		parentFC.Seal()
	}
	return forkFrameContext(targetCtx, parentFC)
}

// ForkFrameContext creates a new frame on ctx inheriting from the frame in ctx.
func ForkFrameContext(ctx context.Context) (context.Context, FrameContext) {
	return OpenFrameContextOn(ctx, ctx)
}

func newFrameContext(parent context.Context) (context.Context, FrameContext) {
	fc := frameContextPool.Get().(*frameContext)
	fc.generation.Add(1)
	fc.sealed.Store(false)
	fc.refcount.Store(1)
	fc.writers.Store(0)
	fc.parent = nil
	values := make(frameValues, 8)
	fc.values.Store(&values)
	return WithFrameContext(parent, fc), fc
}

// forkFrameContext creates a new child frame from a parent frame.
// Uses CAS to increment parent refcount only if still alive, preventing
// a race where FrameFromContext returns a frame that is concurrently released.
// Parent is always sealed when forking, so no lock needed for copying.
func forkFrameContext(ctx context.Context, parent *frameContext) (context.Context, *frameContext) {
	if parent != nil {
		if !tryIncRef(parent) {
			parent = nil
		}
	}

	fc := frameContextPool.Get().(*frameContext)
	fc.generation.Add(1)
	fc.sealed.Store(false)
	fc.refcount.Store(1)
	fc.writers.Store(0)
	fc.parent = parent
	values := make(frameValues, 8)

	// Parent is sealed (checked in OpenFrameContext), safe to read without lock
	if parent != nil {
		parentValues := parent.valuesSnapshot()
		values = make(frameValues, len(parentValues))
		for k, v := range parentValues {
			if ctxKey, ok := k.(*Key); ok && ctxKey.Inherit {
				if cloner, ok := v.(Cloner); ok {
					values[k] = cloner.Clone()
				} else {
					values[k] = v
				}
			}
		}
	}
	fc.values.Store(&values)

	return WithFrameContext(ctx, fc), fc
}
