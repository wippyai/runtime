package interceptor

import (
	"context"
	"errors"
	"testing"

	ctxapi "github.com/ponyruntime/pony/api/context"
	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/runtime"
)

type mockInterceptor struct {
	name        string
	called      bool
	shouldError bool
}

func (m *mockInterceptor) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	m.called = true
	if m.shouldError {
		return &runtime.Result{Error: errors.New("interceptor error")}, ctx
	}
	return next(ctx)
}

func TestChainExecuteNoInterceptors(t *testing.T) {
	chain := newChain(nil)

	executed := false
	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		executed = true
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	rootCtx := ctxapi.NewRootContext()
	task := runtime.Task{}

	ch, err := chain.Execute(rootCtx, mockFunc, task)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if ch == nil {
		t.Fatal("Execute returned nil channel")
	}

	result := <-ch
	if result == nil {
		t.Fatal("channel returned nil result")
	}
	if !executed {
		t.Error("function was not executed")
	}
}

func TestChainExecuteWithInterceptors(t *testing.T) {
	int1 := &mockInterceptor{name: "int1"}
	int2 := &mockInterceptor{name: "int2"}
	int3 := &mockInterceptor{name: "int3"}

	chain := newChain([]apiinterceptor.Interceptor{int1, int2, int3})

	executed := false
	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		executed = true
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	rootCtx := ctxapi.NewRootContext()
	task := runtime.Task{}

	ch, err := chain.Execute(rootCtx, mockFunc, task)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if ch == nil {
		t.Fatal("Execute returned nil channel")
	}

	result := <-ch
	if result == nil {
		t.Fatal("channel returned nil result")
	}

	if !int1.called {
		t.Error("interceptor 1 was not called")
	}
	if !int2.called {
		t.Error("interceptor 2 was not called")
	}
	if !int3.called {
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

	chain := newChain([]apiinterceptor.Interceptor{int1, int2, int3})

	executed := false
	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		executed = true
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	rootCtx := ctxapi.NewRootContext()
	task := runtime.Task{}

	_, err := chain.Execute(rootCtx, mockFunc, task)
	if err == nil {
		t.Fatal("Execute should return error when interceptor errors")
	}

	if !int1.called {
		t.Error("interceptor 1 should be called before error")
	}
	if !int2.called {
		t.Error("interceptor 2 should be called (it's the one that errors)")
	}
	if int3.called {
		t.Error("interceptor 3 should not be called after error")
	}
	if executed {
		t.Error("function should not be executed after interceptor error")
	}
}

func TestChainExecuteFunctionError(t *testing.T) {
	int1 := &mockInterceptor{name: "int1"}

	chain := newChain([]apiinterceptor.Interceptor{int1})

	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		return nil, errors.New("function error")
	}

	rootCtx := ctxapi.NewRootContext()
	task := runtime.Task{}

	_, err := chain.Execute(rootCtx, mockFunc, task)
	if err == nil {
		t.Fatal("Execute should return error when function errors")
	}

	if !int1.called {
		t.Error("interceptor should be called before function error")
	}
}

func TestChainExecuteContextPropagation(t *testing.T) {
	type ctxKey string
	const testKey ctxKey = "test"

	modifyingInterceptor := &struct {
		called bool
	}{}

	interceptor := apiinterceptor.InterceptorFunc(func(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
		modifyingInterceptor.called = true
		newCtx := context.WithValue(ctx, testKey, "modified")
		return next(newCtx)
	})

	chain := newChain([]apiinterceptor.Interceptor{interceptor})

	var receivedCtx context.Context
	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		receivedCtx = ctx
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	rootCtx := ctxapi.NewRootContext()
	task := runtime.Task{}

	_, err := chain.Execute(rootCtx, mockFunc, task)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !modifyingInterceptor.called {
		t.Error("interceptor was not called")
	}

	if receivedCtx == nil {
		t.Fatal("context was not propagated to function")
	}

	if val := receivedCtx.Value(testKey); val != "modified" {
		t.Errorf("context value not propagated, got %v", val)
	}
}
