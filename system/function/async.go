package function

import (
	"context"
	"errors"
	"sync"
	"unsafe"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/runtime"
)

var asyncCallRegistryKey = &ctxapi.Key{Name: "func.async_calls", Inherit: false}

// resultChanPool reduces allocations for result channels in hot path.
var resultChanPool = sync.Pool{
	New: func() any { return make(chan *CallResult, 1) },
}

// CallResult holds the result of an async function call.
type CallResult struct {
	Result *runtime.Result
	Error  error
}

// asyncCallEntry tracks a single async call.
type asyncCallEntry struct {
	cancel context.CancelFunc
	done   chan struct{}
	result any
	err    error
}

// AsyncCallRegistry manages async function calls for a process.
type AsyncCallRegistry struct {
	mu     sync.Mutex
	calls  map[uint64]*asyncCallEntry
	nextID uint64
}

// NewAsyncCallRegistry creates a new registry for async calls.
func NewAsyncCallRegistry() *AsyncCallRegistry {
	return &AsyncCallRegistry{
		calls: make(map[uint64]*asyncCallEntry),
	}
}

// Start begins an async function call and returns immediately with a call ID.
// The call executes in a goroutine and results can be retrieved via Await.
func (r *AsyncCallRegistry) Start(ctx context.Context, registry function.Registry, task runtime.Task) uint64 {
	callCtx, cancel := context.WithCancel(ctx)

	entry := &asyncCallEntry{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	r.mu.Lock()
	r.nextID++
	id := r.nextID
	r.calls[id] = entry
	r.mu.Unlock()

	go func() {
		defer close(entry.done)
		defer cancel()

		result, err := registry.Call(callCtx, task)
		if err != nil {
			entry.err = err
		} else if result != nil {
			if result.Error != nil {
				entry.err = result.Error
			} else {
				entry.result = result.Value
			}
		}
	}()

	return id
}

// Await blocks until the async call completes and returns the result.
func (r *AsyncCallRegistry) Await(ctx context.Context, id uint64) (any, error) {
	r.mu.Lock()
	entry, ok := r.calls[id]
	r.mu.Unlock()

	if !ok {
		return nil, function.ErrCallNotFound
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-entry.done:
		r.mu.Lock()
		delete(r.calls, id)
		r.mu.Unlock()

		if entry.err != nil {
			if errors.Is(entry.err, context.Canceled) {
				return nil, function.ErrCallCancelled
			}
			return nil, entry.err
		}
		return entry.result, nil
	}
}

// Cancel cancels an in-progress async call.
func (r *AsyncCallRegistry) Cancel(id uint64) error {
	r.mu.Lock()
	entry, ok := r.calls[id]
	r.mu.Unlock()

	if !ok {
		return function.ErrCallNotFound
	}

	entry.cancel()
	return nil
}

// Close cancels all pending calls and cleans up resources.
func (r *AsyncCallRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, entry := range r.calls {
		entry.cancel()
		delete(r.calls, id)
	}
}

// GetAsyncCallRegistry retrieves the async call registry from context.
func GetAsyncCallRegistry(ctx context.Context) *AsyncCallRegistry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(asyncCallRegistryKey); ok {
		return val.(*AsyncCallRegistry)
	}
	return nil
}

// SetAsyncCallRegistry stores the async call registry in context.
func SetAsyncCallRegistry(ctx context.Context, r *AsyncCallRegistry) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(asyncCallRegistryKey, r)
}

// GetOrCreateAsyncCallRegistry returns the async call registry from context,
// creating one if it doesn't exist.
func GetOrCreateAsyncCallRegistry(ctx context.Context) *AsyncCallRegistry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(asyncCallRegistryKey); ok {
		return val.(*AsyncCallRegistry)
	}
	r := NewAsyncCallRegistry()
	_ = fc.Set(asyncCallRegistryKey, r)
	return r
}

// CallAsync executes a function call asynchronously using pooled channels.
// Returns a channel that receives the result when the call completes.
// The caller MUST call ReleaseResultChan after reading the result.
func (f *Registry) CallAsync(ctx context.Context, task runtime.Task) (<-chan *CallResult, error) {
	handler, exists := f.handlers.Load(task.ID)
	if !exists {
		return nil, function.NewHandlerNotFoundError(task.ID)
	}

	_, ok := handler.(function.Func)
	if !ok {
		return nil, function.NewInvalidHandlerError(task.ID)
	}

	ch := resultChanPool.Get().(chan *CallResult)

	go func() {
		result, err := f.Call(ctx, task)
		ch <- &CallResult{Result: result, Error: err}
	}()

	return ch, nil
}

// CallAsyncCallback executes a function call asynchronously with a callback.
// This is the most efficient async pattern - no channel allocation.
// The callback is invoked in the goroutine that executes the function.
func (f *Registry) CallAsyncCallback(ctx context.Context, task runtime.Task, callback func(*runtime.Result, error)) error {
	if callback == nil {
		return function.ErrNilCallback
	}

	handler, exists := f.handlers.Load(task.ID)
	if !exists {
		return function.NewHandlerNotFoundError(task.ID)
	}

	_, ok := handler.(function.Func)
	if !ok {
		return function.NewInvalidHandlerError(task.ID)
	}

	go func() {
		result, err := f.Call(ctx, task)
		callback(result, err)
	}()

	return nil
}

// ReleaseResultChan returns a result channel to the pool for reuse.
// Must be called after reading the result from CallAsync.
func ReleaseResultChan(ch <-chan *CallResult) {
	// Convert receive-only channel to bidirectional for pool
	// This is safe because we created it as bidirectional
	c := *(*chan *CallResult)(unsafe.Pointer(&ch))
	select {
	case <-c:
	default:
	}
	resultChanPool.Put(c)
}
