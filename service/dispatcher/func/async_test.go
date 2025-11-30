package funchandler

import (
	"context"
	"errors"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	funcapi "github.com/wippyai/runtime/api/dispatcher/func"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
)

func setupTestContext(reg function.Registry) context.Context {
	ctx := ctxapi.NewRootContext()
	if reg != nil {
		ctx = function.WithRegistry(ctx, reg)
	}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestAsyncStartHandler(t *testing.T) {
	handler := NewAsyncStartHandler()
	mock := &mockRegistry{
		callFn: func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("async result")}, nil
		},
	}

	ctx := setupTestContext(mock)

	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{
			ID: registry.NewID("test", "func"),
		},
	}

	var result funcapi.AsyncStartResponse
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.AsyncStartResponse)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected response error: %v", result.Error)
	}
	if result.CallID == 0 {
		t.Error("expected non-zero CallID")
	}
}

func TestAsyncStartHandlerNoRegistry(t *testing.T) {
	handler := NewAsyncStartHandler()
	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{
			ID: registry.NewID("test", "func"),
		},
	}

	var result funcapi.AsyncStartResponse
	err := handler.Handle(context.Background(), cmd, func(data any) {
		result = data.(funcapi.AsyncStartResponse)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == nil {
		t.Error("expected error for missing registry")
	}
}

func TestAsyncAwaitHandler(t *testing.T) {
	handler := NewAsyncAwaitHandler()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("async result")}, nil
		},
	}

	ctx := setupTestContext(mock)

	callRegistry := GetOrCreateAsyncCallRegistry(ctx)
	callID := callRegistry.Start(ctx, mock, &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	})

	time.Sleep(10 * time.Millisecond)

	awaitCmd := &funcapi.AsyncAwaitCmd{CallID: callID}

	var result funcapi.AsyncAwaitResponse
	err := handler.Handle(ctx, awaitCmd, func(data any) {
		result = data.(funcapi.AsyncAwaitResponse)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected response error: %v", result.Error)
	}
	if result.Cancelled {
		t.Error("result should not be cancelled")
	}
}

func TestAsyncAwaitHandlerNotFound(t *testing.T) {
	handler := NewAsyncAwaitHandler()
	ctx := setupTestContext(nil)
	GetOrCreateAsyncCallRegistry(ctx)

	awaitCmd := &funcapi.AsyncAwaitCmd{CallID: 999}

	var result funcapi.AsyncAwaitResponse
	err := handler.Handle(ctx, awaitCmd, func(data any) {
		result = data.(funcapi.AsyncAwaitResponse)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == nil {
		t.Error("expected error for missing call")
	}
	if !errors.Is(result.Error, ErrCallNotFound) {
		t.Errorf("expected ErrCallNotFound, got %v", result.Error)
	}
}

func TestAsyncCancelHandler(t *testing.T) {
	handler := NewAsyncCancelHandler()
	mock := &mockRegistry{
		callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx := setupTestContext(mock)

	callRegistry := GetOrCreateAsyncCallRegistry(ctx)
	callID := callRegistry.Start(ctx, mock, &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	})

	cancelCmd := &funcapi.AsyncCancelCmd{CallID: callID}

	err := handler.Handle(ctx, cancelCmd, func(data any) {})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAsyncCancelHandlerNotFound(t *testing.T) {
	handler := NewAsyncCancelHandler()
	ctx := setupTestContext(nil)
	GetOrCreateAsyncCallRegistry(ctx)

	cancelCmd := &funcapi.AsyncCancelCmd{CallID: 999}

	err := handler.Handle(ctx, cancelCmd, func(data any) {})

	if err == nil {
		t.Error("expected error for missing call")
	}
	if !errors.Is(err, ErrCallNotFound) {
		t.Errorf("expected ErrCallNotFound, got %v", err)
	}
}

func TestAsyncCallRegistry(t *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := context.Background()
	id := reg.Start(ctx, mock, &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	})

	if id == 0 {
		t.Error("expected non-zero ID")
	}

	result, err := reg.Await(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestAsyncCallRegistryCancel(t *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx := context.Background()
	id := reg.Start(ctx, mock, &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	})

	err := reg.Cancel(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = reg.Await(ctx, id)
	if err == nil {
		t.Error("expected error after cancel")
	}
	if !errors.Is(err, ErrCallCancelled) {
		t.Errorf("expected ErrCallCancelled, got %v", err)
	}
}

func TestAsyncCallRegistryClose(t *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx := context.Background()
	reg.Start(ctx, mock, &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	})
	reg.Start(ctx, mock, &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func2")},
	})

	reg.Close()
}
