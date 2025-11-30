package funchandler

import (
	"context"
	"errors"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	funcapi "github.com/wippyai/runtime/api/dispatcher/func"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
)

type mockRegistry struct {
	callFn func(ctx context.Context, task runtime.Task) (*runtime.Result, error)
}

func (m *mockRegistry) Call(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	if m.callFn != nil {
		return m.callFn(ctx, task)
	}
	return &runtime.Result{}, nil
}

func TestCallHandler(t *testing.T) {
	handler := NewCallHandler()
	mock := &mockRegistry{
		callFn: func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
			if task.ID.String() != "test:func" {
				t.Errorf("expected test:func, got %s", task.ID.String())
			}
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := function.WithRegistry(ctxapi.NewRootContext(), mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{
			ID: registry.NewID("test", "func"),
		},
	}

	var result funcapi.Response
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.Response)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected response error: %v", result.Error)
	}
	if result.Value == nil {
		t.Error("expected non-nil result")
	} else if p, ok := result.Value.(payload.Payload); !ok {
		t.Errorf("expected payload.Payload, got %T", result.Value)
	} else if p.Data() != "result" {
		t.Errorf("expected 'result', got %v", p.Data())
	}
}

func TestCallHandlerNoRegistry(t *testing.T) {
	handler := NewCallHandler()
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{
			ID: registry.NewID("test", "func"),
		},
	}

	var result funcapi.Response
	err := handler.Handle(context.Background(), cmd, func(data any) {
		result = data.(funcapi.Response)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == nil {
		t.Error("expected error for missing registry")
	}
	if !errors.Is(result.Error, ErrRegistryNotFound) {
		t.Errorf("expected ErrRegistryNotFound, got %v", result.Error)
	}
}

func TestCallHandlerError(t *testing.T) {
	handler := NewCallHandler()
	expectedErr := errors.New("call failed")
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return nil, expectedErr
		},
	}

	ctx := function.WithRegistry(ctxapi.NewRootContext(), mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{
			ID: registry.NewID("test", "func"),
		},
	}

	var result funcapi.Response
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.Response)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == nil {
		t.Error("expected error in response")
	}
}

func TestCallHandlerResultError(t *testing.T) {
	handler := NewCallHandler()
	expectedErr := errors.New("result error")
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Error: expectedErr}, nil
		},
	}

	ctx := function.WithRegistry(ctxapi.NewRootContext(), mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{
			ID: registry.NewID("test", "func"),
		},
	}

	var result funcapi.Response
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.Response)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == nil {
		t.Error("expected error in response")
	}
}

func TestService(t *testing.T) {
	svc := NewService()
	if svc.Call == nil {
		t.Error("Call handler should be initialized")
	}
	if svc.AsyncStart == nil {
		t.Error("AsyncStart handler should be initialized")
	}
	if svc.AsyncAwait == nil {
		t.Error("AsyncAwait handler should be initialized")
	}
	if svc.AsyncCancel == nil {
		t.Error("AsyncCancel handler should be initialized")
	}

	count := 0
	expectedIDs := map[dispatcher.CommandID]bool{
		funcapi.CmdCall:        false,
		funcapi.CmdAsyncStart:  false,
		funcapi.CmdAsyncAwait:  false,
		funcapi.CmdAsyncCancel: false,
	}
	svc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		count++
		if _, ok := expectedIDs[id]; !ok {
			t.Errorf("unexpected command ID: %d", id)
		}
		expectedIDs[id] = true
	})

	if count != 4 {
		t.Errorf("expected 4 handlers registered, got %d", count)
	}
	for id, registered := range expectedIDs {
		if !registered {
			t.Errorf("handler for command %d not registered", id)
		}
	}
}
