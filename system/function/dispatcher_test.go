package function

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	funcapi "github.com/wippyai/runtime/api/dispatcher/func"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
)

func setupDispatcherTestContext(reg function.Registry) context.Context {
	ctx := ctxapi.NewRootContext()
	if reg != nil {
		ctx = function.WithRegistry(ctx, reg)
	}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestCallHandler(t *testing.T) {
	handler := &CallHandler{}
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	var result funcapi.Response
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.Response)
	})

	require.NoError(t, err)
	assert.Nil(t, result.Error)
	assert.NotNil(t, result.Value)
}

func TestCallHandler_NoRegistry(t *testing.T) {
	handler := &CallHandler{}
	ctx := context.Background()
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	var result funcapi.Response
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.Response)
	})

	require.NoError(t, err)
	assert.ErrorIs(t, result.Error, ErrRegistryNotFound)
}

func TestCallHandler_Error(t *testing.T) {
	handler := &CallHandler{}
	expectedErr := errors.New("call error")
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return nil, expectedErr
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	var result funcapi.Response
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.Response)
	})

	require.NoError(t, err)
	assert.ErrorIs(t, result.Error, expectedErr)
}

func TestCallHandler_ResultError(t *testing.T) {
	handler := &CallHandler{}
	expectedErr := errors.New("result error")
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Error: expectedErr}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	var result funcapi.Response
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.Response)
	})

	require.NoError(t, err)
	assert.ErrorIs(t, result.Error, expectedErr)
}

func TestAsyncStartHandler(t *testing.T) {
	handler := &AsyncStartHandler{}
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("async result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	var result funcapi.AsyncStartResponse
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.AsyncStartResponse)
	})

	require.NoError(t, err)
	assert.Nil(t, result.Error)
	assert.NotZero(t, result.CallID)
}

func TestAsyncStartHandler_NoRegistry(t *testing.T) {
	handler := &AsyncStartHandler{}
	ctx := context.Background()
	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	var result funcapi.AsyncStartResponse
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.AsyncStartResponse)
	})

	require.NoError(t, err)
	assert.ErrorIs(t, result.Error, ErrRegistryNotFound)
}

func TestAsyncAwaitHandler(t *testing.T) {
	handler := &AsyncAwaitHandler{}
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("async result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)

	callRegistry := GetOrCreateAsyncCallRegistry(ctx)
	callID := callRegistry.Start(ctx, mock, runtime.Task{ID: registry.NewID("test", "func")})

	time.Sleep(10 * time.Millisecond)

	cmd := &funcapi.AsyncAwaitCmd{CallID: callID}

	var result funcapi.AsyncAwaitResponse
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.AsyncAwaitResponse)
	})

	require.NoError(t, err)
	assert.Nil(t, result.Error)
	assert.False(t, result.Cancelled)
	assert.NotNil(t, result.Value)
}

func TestAsyncAwaitHandler_NotFound(t *testing.T) {
	handler := &AsyncAwaitHandler{}
	ctx := setupDispatcherTestContext(nil)
	GetOrCreateAsyncCallRegistry(ctx)

	cmd := &funcapi.AsyncAwaitCmd{CallID: 999}

	var result funcapi.AsyncAwaitResponse
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.AsyncAwaitResponse)
	})

	require.NoError(t, err)
	assert.ErrorIs(t, result.Error, ErrCallNotFound)
}

func TestAsyncAwaitHandler_NoRegistry(t *testing.T) {
	handler := &AsyncAwaitHandler{}
	ctx := context.Background()
	cmd := &funcapi.AsyncAwaitCmd{CallID: 1}

	var result funcapi.AsyncAwaitResponse
	err := handler.Handle(ctx, cmd, func(data any) {
		result = data.(funcapi.AsyncAwaitResponse)
	})

	require.NoError(t, err)
	assert.ErrorIs(t, result.Error, ErrCallNotFound)
}

func TestAsyncCancelHandler(t *testing.T) {
	handler := &AsyncCancelHandler{}
	mock := &mockRegistry{
		callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx := setupDispatcherTestContext(mock)

	callRegistry := GetOrCreateAsyncCallRegistry(ctx)
	callID := callRegistry.Start(ctx, mock, runtime.Task{ID: registry.NewID("test", "func")})

	cmd := &funcapi.AsyncCancelCmd{CallID: callID}

	err := handler.Handle(ctx, cmd, func(_ any) {})
	require.NoError(t, err)
}

func TestAsyncCancelHandler_NotFound(t *testing.T) {
	handler := &AsyncCancelHandler{}
	ctx := setupDispatcherTestContext(nil)
	GetOrCreateAsyncCallRegistry(ctx)

	cmd := &funcapi.AsyncCancelCmd{CallID: 999}

	err := handler.Handle(ctx, cmd, func(_ any) {})
	assert.ErrorIs(t, err, ErrCallNotFound)
}

func TestAsyncCancelHandler_NoRegistry(t *testing.T) {
	handler := &AsyncCancelHandler{}
	ctx := context.Background()
	cmd := &funcapi.AsyncCancelCmd{CallID: 1}

	err := handler.Handle(ctx, cmd, func(_ any) {})
	assert.ErrorIs(t, err, ErrCallNotFound)
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher()

	registered := make(map[uint16]bool)
	register := func(id dispatcher.CommandID, _ dispatcher.Handler) {
		registered[uint16(id)] = true
	}

	d.RegisterAll(register)

	assert.True(t, registered[uint16(funcapi.CmdCall)])
	assert.True(t, registered[uint16(funcapi.CmdAsyncStart)])
	assert.True(t, registered[uint16(funcapi.CmdAsyncAwait)])
	assert.True(t, registered[uint16(funcapi.CmdAsyncCancel)])
}

func BenchmarkCallHandler(b *testing.B) {
	handler := &CallHandler{}
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}
	emit := func(_ any) {}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.Handle(ctx, cmd, emit)
	}
}

func BenchmarkCallHandler_Parallel(b *testing.B) {
	handler := &CallHandler{}
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}
	emit := func(_ any) {}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = handler.Handle(ctx, cmd, emit)
		}
	})
}

// Stress tests

func TestCallHandler_Stress(t *testing.T) {
	handler := &CallHandler{}
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	const numCalls = 1000
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()
			var result funcapi.Response
			err := handler.Handle(ctx, cmd, func(data any) {
				result = data.(funcapi.Response)
			})
			assert.NoError(t, err)
			assert.Nil(t, result.Error)
			assert.NotNil(t, result.Value)
		}()
	}

	wg.Wait()
}

func TestAsyncStartHandler_Stress(t *testing.T) {
	handler := &AsyncStartHandler{}
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	const numCalls = 500
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()
			var result funcapi.AsyncStartResponse
			err := handler.Handle(ctx, cmd, func(data any) {
				result = data.(funcapi.AsyncStartResponse)
			})
			assert.NoError(t, err)
			assert.Nil(t, result.Error)
			assert.NotZero(t, result.CallID)
		}()
	}

	wg.Wait()
}

func TestDispatcher_FullCycle_Stress(t *testing.T) {
	// Use a channel to control when the mock function completes
	// This ensures the async call is still in progress when we call Await
	ready := make(chan struct{})
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			<-ready
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)

	startHandler := &AsyncStartHandler{}
	awaitHandler := &AsyncAwaitHandler{}

	const numCalls = 200
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()

			var startResult funcapi.AsyncStartResponse
			err := startHandler.Handle(ctx, &funcapi.AsyncStartCmd{
				Task: runtime.Task{ID: registry.NewID("test", "func")},
			}, func(data any) {
				startResult = data.(funcapi.AsyncStartResponse)
			})
			require.NoError(t, err)
			require.Nil(t, startResult.Error)

			// Signal the mock to complete
			ready <- struct{}{}

			var awaitResult funcapi.AsyncAwaitResponse
			err = awaitHandler.Handle(ctx, &funcapi.AsyncAwaitCmd{
				CallID: startResult.CallID,
			}, func(data any) {
				awaitResult = data.(funcapi.AsyncAwaitResponse)
			})
			require.NoError(t, err)
			assert.Nil(t, awaitResult.Error)
			assert.False(t, awaitResult.Cancelled)
		}()
	}

	wg.Wait()
}

func BenchmarkAsyncStartHandler(b *testing.B) {
	handler := &AsyncStartHandler{}
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}
	emit := func(_ any) {}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.Handle(ctx, cmd, emit)
	}
}

func BenchmarkAsyncStartHandler_Parallel(b *testing.B) {
	handler := &AsyncStartHandler{}
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}
	emit := func(_ any) {}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = handler.Handle(ctx, cmd, emit)
		}
	})
}
