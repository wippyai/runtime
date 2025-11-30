package funchandler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	funcapi "github.com/wippyai/runtime/api/dispatcher/func"
	"github.com/wippyai/runtime/api/function"
)

var AsyncCallRegistryKey = &ctxapi.Key{Name: "func.async_calls", Inherit: false}

var (
	ErrCallNotFound  = errors.New("async call not found")
	ErrCallCancelled = errors.New("async call cancelled")
)

type asyncCallEntry struct {
	cancel   context.CancelFunc
	done     chan struct{}
	result   any
	err      error
	complete atomic.Bool
}

type AsyncCallRegistry struct {
	mu     sync.Mutex
	calls  map[uint64]*asyncCallEntry
	nextID uint64
}

func NewAsyncCallRegistry() *AsyncCallRegistry {
	return &AsyncCallRegistry{
		calls: make(map[uint64]*asyncCallEntry),
	}
}

func (r *AsyncCallRegistry) Start(ctx context.Context, registry function.Registry, task *funcapi.AsyncStartCmd) uint64 {
	r.mu.Lock()
	r.nextID++
	id := r.nextID

	callCtx, cancel := context.WithCancel(ctx)
	entry := &asyncCallEntry{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	r.calls[id] = entry
	r.mu.Unlock()

	// hmmmmm, todo: amnything we can to make it faster for all consumers fron low level interface, including interceptrors?
	go func() {
		defer close(entry.done)
		defer cancel()

		result, err := registry.Call(callCtx, task.Task)
		if err != nil {
			entry.err = err
		} else if result != nil {
			if result.Error != nil {
				entry.err = result.Error
			} else {
				entry.result = result.Value
			}
		}
		entry.complete.Store(true)
	}()

	return id
}

func (r *AsyncCallRegistry) Await(ctx context.Context, id uint64) (any, error) {
	r.mu.Lock()
	entry, ok := r.calls[id]
	r.mu.Unlock()

	if !ok {
		return nil, ErrCallNotFound
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
				return nil, ErrCallCancelled
			}
			return nil, entry.err
		}
		return entry.result, nil
	}
}

func (r *AsyncCallRegistry) Cancel(id uint64) error {
	r.mu.Lock()
	entry, ok := r.calls[id]
	r.mu.Unlock()

	if !ok {
		return ErrCallNotFound
	}

	entry.cancel()
	return nil
}

func (r *AsyncCallRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, entry := range r.calls {
		entry.cancel()
		delete(r.calls, id)
	}
}

func GetAsyncCallRegistry(ctx context.Context) *AsyncCallRegistry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(AsyncCallRegistryKey); ok {
		return val.(*AsyncCallRegistry)
	}
	return nil
}

func SetAsyncCallRegistry(ctx context.Context, r *AsyncCallRegistry) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(AsyncCallRegistryKey, r)
}

func GetOrCreateAsyncCallRegistry(ctx context.Context) *AsyncCallRegistry {
	if r := GetAsyncCallRegistry(ctx); r != nil {
		return r
	}
	r := NewAsyncCallRegistry()
	SetAsyncCallRegistry(ctx, r)
	return r
}

type AsyncStartHandler struct{}

func NewAsyncStartHandler() *AsyncStartHandler {
	return &AsyncStartHandler{}
}

func (h *AsyncStartHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	startCmd := cmd.(*funcapi.AsyncStartCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		emit(funcapi.AsyncStartResponse{Error: ErrRegistryNotFound})
		return nil
	}

	callRegistry := GetOrCreateAsyncCallRegistry(ctx)
	id := callRegistry.Start(ctx, registry, startCmd)
	emit(funcapi.AsyncStartResponse{CallID: id})
	return nil
}

type AsyncAwaitHandler struct{}

func NewAsyncAwaitHandler() *AsyncAwaitHandler {
	return &AsyncAwaitHandler{}
}

func (h *AsyncAwaitHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	awaitCmd := cmd.(*funcapi.AsyncAwaitCmd)

	callRegistry := GetAsyncCallRegistry(ctx)
	if callRegistry == nil {
		emit(funcapi.AsyncAwaitResponse{Error: ErrCallNotFound})
		return nil
	}

	result, err := callRegistry.Await(ctx, awaitCmd.CallID)
	if err != nil {
		cancelled := errors.Is(err, ErrCallCancelled)
		emit(funcapi.AsyncAwaitResponse{Error: err, Cancelled: cancelled})
		return nil
	}

	emit(funcapi.AsyncAwaitResponse{Value: result})
	return nil
}

type AsyncCancelHandler struct{}

func NewAsyncCancelHandler() *AsyncCancelHandler {
	return &AsyncCancelHandler{}
}

func (h *AsyncCancelHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	cancelCmd := cmd.(*funcapi.AsyncCancelCmd)

	callRegistry := GetAsyncCallRegistry(ctx)
	if callRegistry == nil {
		return ErrCallNotFound
	}

	return callRegistry.Cancel(cancelCmd.CallID)
}
