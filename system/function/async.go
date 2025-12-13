package function

import (
	"context"
	"errors"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/runtime"
)

var asyncCallRegistryKey = &ctxapi.Key{Name: "func.async_calls", Inherit: false}

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
