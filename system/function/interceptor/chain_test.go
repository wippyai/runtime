package interceptor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

type mockInterceptor struct {
	name        string
	called      atomic.Bool
	shouldError bool
}

func (m *mockInterceptor) Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	m.called.Store(true)
	if m.shouldError {
		return nil, errors.New("interceptor error")
	}
	return next(ctx, task)
}

func TestChainExecuteNoInterceptors(t *testing.T) {
	chain := newChain(nil, zap.NewNop())

	executed := false
	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		executed = true
		return &runtime.Result{}, nil
	}

	rootCtx := ctxapi.NewRootContext()
	task := runtime.Task{}

	result, err := chain.Execute(rootCtx, mockFunc, task)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil {
		t.Fatal("Execute returned nil result")
	}
	if !executed {
		t.Error("function was not executed")
	}
}

func TestChainExecuteWithInterceptors(t *testing.T) {
	int1 := &mockInterceptor{name: "int1"}
	int2 := &mockInterceptor{name: "int2"}
	int3 := &mockInterceptor{name: "int3"}

	chain := newChain([]function.Interceptor{int1, int2, int3}, zap.NewNop())

	executed := false
	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		executed = true
		return &runtime.Result{}, nil
	}

	rootCtx := ctxapi.NewRootContext()
	task := runtime.Task{}

	result, err := chain.Execute(rootCtx, mockFunc, task)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil {
		t.Fatal("Execute returned nil result")
	}

	if !int1.called.Load() {
		t.Error("interceptor 1 was not called")
	}
	if !int2.called.Load() {
		t.Error("interceptor 2 was not called")
	}
	if !int3.called.Load() {
		t.Error("interceptor 3 was not called")
	}
	if !executed {
		t.Error("function was not executed")
	}
}

func TestChainExecuteInterceptorError(t *testing.T) {
	int1 := &mockInterceptor{name: "int1"}
	int2 := &mockInterceptor{name: "int2", shouldError: true}
	int3 := &mockInterceptor{name: "int3"}

	chain := newChain([]function.Interceptor{int1, int2, int3}, zap.NewNop())

	executed := false
	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		executed = true
		return &runtime.Result{}, nil
	}

	rootCtx := ctxapi.NewRootContext()
	task := runtime.Task{}

	_, err := chain.Execute(rootCtx, mockFunc, task)
	if err == nil {
		t.Fatal("Execute should return error when interceptor errors")
	}

	if !int1.called.Load() {
		t.Error("interceptor 1 should be called before error")
	}
	if !int2.called.Load() {
		t.Error("interceptor 2 should be called (it's the one that errors)")
	}
	if int3.called.Load() {
		t.Error("interceptor 3 should not be called after error")
	}
	if executed {
		t.Error("function should not be executed after interceptor error")
	}
}

func TestChainExecuteFunctionError(t *testing.T) {
	int1 := &mockInterceptor{name: "int1"}

	chain := newChain([]function.Interceptor{int1}, zap.NewNop())

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return nil, errors.New("function error")
	}

	rootCtx := ctxapi.NewRootContext()
	task := runtime.Task{}

	_, err := chain.Execute(rootCtx, mockFunc, task)
	if err == nil {
		t.Fatal("Execute should return error when function errors")
	}

	if !int1.called.Load() {
		t.Error("interceptor should be called before function error")
	}
}

func TestChainExecuteContextPropagation(t *testing.T) {
	type ctxKey string
	const testKey ctxKey = "test"

	interceptor := &testModifyingInterceptor{
		key:   testKey,
		value: "modified",
	}

	chain := newChain([]function.Interceptor{interceptor}, zap.NewNop())

	var receivedCtx context.Context
	mockFunc := func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		receivedCtx = ctx
		return &runtime.Result{}, nil
	}

	rootCtx := ctxapi.NewRootContext()
	task := runtime.Task{}

	_, err := chain.Execute(rootCtx, mockFunc, task)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !interceptor.called {
		t.Error("interceptor was not called")
	}

	if receivedCtx == nil {
		t.Fatal("context was not propagated to function")
	}

	if val := receivedCtx.Value(testKey); val != "modified" {
		t.Errorf("context value not propagated, got %v", val)
	}
}

// testModifyingInterceptor modifies context for testing
type testModifyingInterceptor struct {
	key    interface{}
	value  interface{}
	called bool
}

func (m *testModifyingInterceptor) Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	m.called = true
	newCtx := context.WithValue(ctx, m.key, m.value)
	return next(newCtx, task)
}
