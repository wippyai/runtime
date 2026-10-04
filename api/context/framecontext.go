// SPDX-License-Identifier: MPL-2.0

package context

import (
	"context"
	"reflect"
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

// borrowedValue keeps ownership in the same immutable snapshot as the value.
// Ordinary frames store values directly; only continuations use this marker.
type borrowedValue struct{ value any }

func frameValue(value any) any {
	if borrowed, ok := value.(*borrowedValue); ok {
		return borrowed.value
	}
	return value
}

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
	// execution is set on the frame of a later code incarnation: the
	// execution's own frame, which owns and closes the Execution values.
	execution FrameContext
	// borrowed is immutable after publication. It retains the original shared
	// handles so restoring one after a replacement restores borrowing too.
	borrowed frameValues
}

type frameContextRef struct {
	frame      *frameContext
	generation uint64
	released   atomic.Bool
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
	return frameValue(val), exists
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
		next[key] = f.storedValue(key, value)
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
			next[p.Key] = f.storedValue(p.Key, p.Value)
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

// Writes own their values, except when re-setting an identical comparable
// handle borrowed from the parent. The marker is swapped with the value, not
// updated separately, so concurrent writes cannot reorder ownership changes.
func (f *frameContext) storedValue(key, value any) any {
	if borrowed, ok := f.borrowed[key].(*borrowedValue); ok {
		if reflect.ValueOf(borrowed.value).Comparable() && reflect.ValueOf(value).Comparable() && borrowed.value == value {
			return borrowed
		}
	}
	return value
}

func (f *frameContext) Has(key any) bool {
	values := f.valuesSnapshot()
	_, exists := values[key]
	return exists
}

func (f *frameContext) Iterate(fn func(key any, value any)) {
	for k, v := range f.valuesSnapshot() {
		fn(k, frameValue(v))
	}
}

func (f *frameContext) InheritablePairs() []Pair {
	var pairs []Pair
	for k, v := range f.valuesSnapshot() {
		if ctxKey, ok := k.(*Key); ok && ctxKey.Inherit {
			pairs = append(pairs, Pair{Key: k, Value: frameValue(v)})
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
	if f == nil {
		return nil
	}
	releaseFrame(f, f.generation.Load())
	return nil
}

func (r *frameContextRef) resolveFrame() *frameContext {
	if r == nil || r.frame == nil {
		return nil
	}
	if r.generation != r.frame.generation.Load() {
		return nil
	}
	if r.frame.refcount.Load() <= 0 {
		return nil
	}
	return r.frame
}

func (r *frameContextRef) Get(key any) (any, bool) {
	frame := r.resolveFrame()
	if frame == nil {
		return nil, false
	}
	return frame.Get(key)
}

func (r *frameContextRef) Has(key any) bool {
	frame := r.resolveFrame()
	if frame == nil {
		return false
	}
	return frame.Has(key)
}

func (r *frameContextRef) Set(key any, value any) error {
	frame := r.resolveFrame()
	if frame == nil {
		return NewFrameSealedError(key)
	}
	return frame.Set(key, value)
}

func (r *frameContextRef) SetMultiple(pairs ...Pair) error {
	frame := r.resolveFrame()
	if frame == nil {
		return ErrFrameSealed
	}
	return frame.SetMultiple(pairs...)
}

func (r *frameContextRef) Iterate(fn func(key any, value any)) {
	frame := r.resolveFrame()
	if frame == nil {
		return
	}
	frame.Iterate(fn)
}

func (r *frameContextRef) InheritablePairs() []Pair {
	frame := r.resolveFrame()
	if frame == nil {
		return nil
	}
	return frame.InheritablePairs()
}

func (r *frameContextRef) Seal() {
	frame := r.resolveFrame()
	if frame == nil {
		return
	}
	frame.Seal()
}

func (r *frameContextRef) IsSealed() bool {
	frame := r.resolveFrame()
	if frame == nil {
		return true
	}
	return frame.IsSealed()
}

func (r *frameContextRef) Close() error {
	if r == nil {
		return nil
	}
	if !r.released.CompareAndSwap(false, true) {
		return nil
	}
	releaseFrame(r.frame, r.generation)
	return nil
}

func newFrameRef(frame *frameContext) *frameContextRef {
	if frame == nil {
		return nil
	}
	return &frameContextRef{
		frame:      frame,
		generation: frame.generation.Load(),
	}
}

// newBorrowedFrameRef creates a non-owning reference.
// Close() is a no-op so borrowed handles cannot accidentally release shared owners.
func newBorrowedFrameRef(frame *frameContext, generation uint64) *frameContextRef {
	if frame == nil {
		return nil
	}
	ref := &frameContextRef{
		frame:      frame,
		generation: generation,
	}
	ref.released.Store(true)
	return ref
}

// WithFrameContext attaches FrameContext to the provided context.
func WithFrameContext(ctx context.Context, fc FrameContext) context.Context {
	switch f := fc.(type) {
	case *frameContextRef:
		return context.WithValue(ctx, frameContextKey, f)
	case *frameContext:
		return context.WithValue(ctx, frameContextKey, newFrameRef(f))
	default:
		return context.WithValue(ctx, frameContextKey, fc)
	}
}

// FrameFromContext extracts FrameContext from context.
func FrameFromContext(ctx context.Context) FrameContext {
	switch v := ctx.Value(frameContextKey).(type) {
	case *frameContextRef:
		if v.resolveFrame() == nil {
			return nil
		}
		return v
	case frameContextRef:
		// Legacy by-value storage from older binaries.
		ref := &frameContextRef{frame: v.frame, generation: v.generation}
		// No stable ownership token in by-value form; force non-owner semantics.
		ref.released.Store(true)
		if ref.resolveFrame() == nil {
			return nil
		}
		return ref
	case *frameContext:
		// Backward compatibility for contexts created before frameContextRef.
		ref := newFrameRef(v)
		if ref == nil || ref.resolveFrame() == nil {
			return nil
		}
		return ref
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

// ContinueFrameContext opens the frame of the next code incarnation of the
// execution running in ctx, as when a process upgrades: a fork of the
// execution's frame that also carries its Execution values, holding a
// reference on that frame until it is released. The values stay owned by the
// execution's frame: releasing the continuation does not close them. An
// execution without a frame has no values to carry and continues in a new
// root frame.
func ContinueFrameContext(ctx context.Context) (context.Context, FrameContext, error) {
	fc := FrameFromContext(ctx)
	if fc == nil {
		next, nfc := OpenFrameContext(ctx)
		return next, nfc, nil
	}
	// The previous incarnation has finished with the execution's frame; it
	// is sealed so the continuation is always a fork, never the frame itself.
	fc.Seal()
	if ref, ok := fc.(*frameContextRef); ok {
		next, frame := forkFrameContextMode(ctx, ref, true)
		return next, frame, nil
	}
	var carried []Pair
	fc.Iterate(func(key, value any) {
		if k, ok := key.(*Key); ok && k.Execution {
			carried = append(carried, Pair{Key: key, Value: value})
		}
	})
	next, nfc := OpenFrameContext(ctx)
	if len(carried) > 0 {
		if err := nfc.SetMultiple(carried...); err != nil {
			ReleaseFrameContext(nfc)
			return ctx, nil, err
		}
	}
	if ref, ok := nfc.(*frameContextRef); ok {
		if frame := ref.resolveFrame(); frame != nil {
			frame.execution = ExecutionFrame(ctx)
			values := cloneValues(frame.valuesSnapshot(), 0)
			frame.borrowed = make(frameValues, len(carried))
			for _, pair := range carried {
				borrowed := &borrowedValue{value: pair.Value}
				values[pair.Key] = borrowed
				frame.borrowed[pair.Key] = borrowed
			}
			frame.values.Store(&values)
		}
	}
	return next, nfc, nil
}

// ExecutionFrame returns the frame of the execution running in ctx: the
// frame its host created, also when ctx belongs to a later code incarnation
// (see ContinueFrameContext). It is nil when ctx has no frame.
func ExecutionFrame(ctx context.Context) FrameContext {
	fc := FrameFromContext(ctx)
	if ref, ok := fc.(*frameContextRef); ok {
		if frame := ref.resolveFrame(); frame != nil && frame.execution != nil {
			return frame.execution
		}
	}
	return fc
}

// ReleaseFrameContext decrements refcount and triggers chain collapse when zero.
// Only pools the frame when all references (including children) are released.
func ReleaseFrameContext(fc FrameContext) {
	if fc == nil {
		return
	}
	_ = fc.Close()
}

// tryIncRef atomically increments refcount only if the frame is still alive (refcount > 0).
// Returns false if the frame has already been released, preventing use-after-pool races.
func tryIncRef(f *frameContext, expectedGeneration uint64) bool {
	for {
		if expectedGeneration != 0 && f.generation.Load() != expectedGeneration {
			return false
		}
		current := f.refcount.Load()
		if current <= 0 {
			return false
		}
		if expectedGeneration != 0 && f.generation.Load() != expectedGeneration {
			return false
		}
		if f.refcount.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func releaseFrame(f *frameContext, expectedGeneration uint64) {
	if f == nil {
		return
	}

	for {
		if expectedGeneration != 0 && f.generation.Load() != expectedGeneration {
			return
		}
		current := f.refcount.Load()
		if current <= 0 {
			return
		}
		next := current - 1
		if !f.refcount.CompareAndSwap(current, next) {
			continue
		}
		if next != 0 {
			return
		}
		break
	}

	// Invalidate all context-bound references before clearing/repooling.
	f.generation.Add(1)

	// refcount == 0 means exclusive access, no lock needed
	var closers []Closer
	for _, v := range f.valuesSnapshot() {
		closer, ok := v.(Closer)
		if !ok {
			continue
		}
		closers = append(closers, closer)
	}
	f.execution = nil
	f.borrowed = nil
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
		releaseFrame(parent, 0)
	}

	frameContextPool.Put(f)
}

// OpenFrameContext ensures a usable frame for the provided context.
// If an unsealed frame already exists, it returns a borrowed handle to that frame
// (Close is a no-op, ownership stays with the context owner).
// Otherwise, it creates a new owned frame (forking from sealed parent when present).
func OpenFrameContext(ctx context.Context) (context.Context, FrameContext) {
	fc := FrameFromContext(ctx)
	if fc != nil && !fc.IsSealed() {
		if ref, ok := fc.(*frameContextRef); ok {
			if frame := ref.resolveFrame(); frame != nil {
				return ctx, newBorrowedFrameRef(frame, ref.generation)
			}
		}
		return ctx, fc
	}

	parentRef, _ := fc.(*frameContextRef)
	newCtx, newFC := forkFrameContext(ctx, parentRef)
	return newCtx, newFC
}

// OpenFrameContextOn creates a new frame on targetCtx, inheriting from parentCtx.
// Uses reference counting to ensure parent frame stays alive while child iterates.
func OpenFrameContextOn(targetCtx context.Context, parentCtx context.Context) (context.Context, FrameContext) {
	parentRef, _ := FrameFromContext(parentCtx).(*frameContextRef)
	parentFC := parentRef.resolveFrame()
	if parentFC != nil && !parentFC.IsSealed() {
		// Forking implies immutable parent snapshot for safe copy-on-fork.
		parentFC.Seal()
	}
	return forkFrameContext(targetCtx, parentRef)
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
	fc.execution = nil
	fc.borrowed = nil
	values := make(frameValues, 8)
	fc.values.Store(&values)
	ref := newFrameRef(fc)
	return WithFrameContext(parent, ref), ref
}

// forkFrameContext creates a new child frame from a parent frame.
// Uses CAS to increment parent refcount only if still alive, preventing
// a race where FrameFromContext returns a frame that is concurrently released.
// Parent is always sealed when forking, so no lock needed for copying.
func forkFrameContext(ctx context.Context, parentRef *frameContextRef) (context.Context, *frameContextRef) {
	return forkFrameContextMode(ctx, parentRef, false)
}

// A continuation shares execution-owned values; a normal fork applies the
// existing inheritance/Clone rules. Both acquire the same parent reference.
func forkFrameContextMode(ctx context.Context, parentRef *frameContextRef, continuing bool) (context.Context, *frameContextRef) {
	parent := parentRef.resolveFrame()
	if parent != nil {
		if !tryIncRef(parent, parentRef.generation) {
			parent = nil
		}
	}

	fc := frameContextPool.Get().(*frameContext)
	fc.generation.Add(1)
	fc.sealed.Store(false)
	fc.refcount.Store(1)
	fc.writers.Store(0)
	fc.parent = parent
	fc.execution = nil
	fc.borrowed = nil
	values := make(frameValues, 8)

	// Parent is sealed (checked in OpenFrameContext), safe to read without lock
	if parent != nil {
		parentValues := parent.valuesSnapshot()
		values = make(frameValues, len(parentValues))
		for k, v := range parentValues {
			v = frameValue(v)
			ctxKey, ok := k.(*Key)
			if !ok {
				continue
			}
			if continuing && ctxKey.Execution {
				if fc.borrowed == nil {
					fc.borrowed = make(frameValues)
				}
				borrowed := &borrowedValue{value: v}
				values[k] = borrowed
				fc.borrowed[k] = borrowed
			} else if ctxKey.Inherit {
				if cloner, ok := v.(Cloner); ok {
					values[k] = cloner.Clone()
				} else {
					values[k] = v
				}
			}
		}
		if continuing {
			fc.execution = ExecutionFrame(ctx)
		}
	}
	fc.values.Store(&values)

	ref := newFrameRef(fc)
	return WithFrameContext(ctx, ref), ref
}
