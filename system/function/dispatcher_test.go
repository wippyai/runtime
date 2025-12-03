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
	_ = SetAsyncCallRegistry(ctx, NewAsyncCallRegistry())
	return ctx
}

type emitFunc func(data any, err error)

func (f emitFunc) Emit(data any, err error) { f(data, err) }

func TestCallHandler(t *testing.T) {
	d := NewDispatcher()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	done := make(chan funcapi.Response, 1)
	err := d.call.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
		done <- data.(funcapi.Response)
	}))

	require.NoError(t, err)
	result := <-done
	assert.Nil(t, result.Error)
	assert.NotNil(t, result.Value)
}

func TestCallHandler_NoRegistry(t *testing.T) {
	d := NewDispatcher()
	ctx := context.Background()
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	done := make(chan funcapi.Response, 1)
	err := d.call.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
		done <- data.(funcapi.Response)
	}))

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, function.ErrRegistryNotFound)
}

func TestCallHandler_Error(t *testing.T) {
	d := NewDispatcher()
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

	done := make(chan funcapi.Response, 1)
	err := d.call.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
		done <- data.(funcapi.Response)
	}))

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, expectedErr)
}

func TestCallHandler_ResultError(t *testing.T) {
	d := NewDispatcher()
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

	done := make(chan funcapi.Response, 1)
	err := d.call.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
		done <- data.(funcapi.Response)
	}))

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, expectedErr)
}

func TestAsyncStartHandler(t *testing.T) {
	d := NewDispatcher()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("async result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	done := make(chan funcapi.AsyncStartResponse, 1)
	err := d.asyncStart.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
		done <- data.(funcapi.AsyncStartResponse)
	}))

	require.NoError(t, err)
	result := <-done
	assert.Nil(t, result.Error)
	assert.NotZero(t, result.CallID)
}

func TestAsyncStartHandler_NoRegistry(t *testing.T) {
	d := NewDispatcher()
	ctx := context.Background()
	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}

	done := make(chan funcapi.AsyncStartResponse, 1)
	err := d.asyncStart.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
		done <- data.(funcapi.AsyncStartResponse)
	}))

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, function.ErrRegistryNotFound)
}

func TestAsyncAwaitHandler(t *testing.T) {
	d := NewDispatcher()
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

	done := make(chan funcapi.AsyncAwaitResponse, 1)
	err := d.asyncAwait.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
		done <- data.(funcapi.AsyncAwaitResponse)
	}))

	require.NoError(t, err)
	result := <-done
	assert.Nil(t, result.Error)
	assert.False(t, result.Cancelled)
	assert.NotNil(t, result.Value)
}

func TestAsyncAwaitHandler_NotFound(t *testing.T) {
	d := NewDispatcher()
	ctx := setupDispatcherTestContext(nil)
	GetOrCreateAsyncCallRegistry(ctx)

	cmd := &funcapi.AsyncAwaitCmd{CallID: 999}

	done := make(chan funcapi.AsyncAwaitResponse, 1)
	err := d.asyncAwait.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
		done <- data.(funcapi.AsyncAwaitResponse)
	}))

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, function.ErrCallNotFound)
}

func TestAsyncAwaitHandler_NoRegistry(t *testing.T) {
	d := NewDispatcher()
	ctx := context.Background()
	cmd := &funcapi.AsyncAwaitCmd{CallID: 1}

	done := make(chan funcapi.AsyncAwaitResponse, 1)
	err := d.asyncAwait.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
		done <- data.(funcapi.AsyncAwaitResponse)
	}))

	require.NoError(t, err)
	result := <-done
	assert.ErrorIs(t, result.Error, function.ErrCallNotFound)
}

func TestAsyncCancelHandler(t *testing.T) {
	d := NewDispatcher()
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

	done := make(chan struct{}, 1)
	err := d.asyncCancel.Handle(ctx, cmd, emitFunc(func(_ any, _ error) {
		done <- struct{}{}
	}))
	require.NoError(t, err)
	<-done
}

func TestAsyncCancelHandler_NotFound(t *testing.T) {
	d := NewDispatcher()
	ctx := setupDispatcherTestContext(nil)
	GetOrCreateAsyncCallRegistry(ctx)

	cmd := &funcapi.AsyncCancelCmd{CallID: 999}

	done := make(chan struct{}, 1)
	err := d.asyncCancel.Handle(ctx, cmd, emitFunc(func(_ any, _ error) {
		done <- struct{}{}
	}))
	require.NoError(t, err)
	<-done
}

func TestAsyncCancelHandler_NoRegistry(t *testing.T) {
	d := NewDispatcher()
	ctx := context.Background()
	cmd := &funcapi.AsyncCancelCmd{CallID: 1}

	done := make(chan struct{}, 1)
	err := d.asyncCancel.Handle(ctx, cmd, emitFunc(func(_ any, _ error) {
		done <- struct{}{}
	}))
	require.NoError(t, err)
	<-done
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
	d := NewDispatcher()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}
	emit := emitFunc(func(_ any, _ error) {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.call.Handle(ctx, cmd, emit)
	}
}

func BenchmarkCallHandler_Parallel(b *testing.B) {
	d := NewDispatcher()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.CallCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}
	emit := emitFunc(func(_ any, _ error) {})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = d.call.Handle(ctx, cmd, emit)
		}
	})
}

// Stress tests

func TestCallHandler_Stress(t *testing.T) {
	d := NewDispatcher()
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
			done := make(chan funcapi.Response, 1)
			err := d.call.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
				done <- data.(funcapi.Response)
			}))
			assert.NoError(t, err)
			result := <-done
			assert.Nil(t, result.Error)
			assert.NotNil(t, result.Value)
		}()
	}

	wg.Wait()
}

func TestAsyncStartHandler_Stress(t *testing.T) {
	d := NewDispatcher()
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
			done := make(chan funcapi.AsyncStartResponse, 1)
			err := d.asyncStart.Handle(ctx, cmd, emitFunc(func(data any, _ error) {
				done <- data.(funcapi.AsyncStartResponse)
			}))
			assert.NoError(t, err)
			result := <-done
			assert.Nil(t, result.Error)
			assert.NotZero(t, result.CallID)
		}()
	}

	wg.Wait()
}

func TestDispatcher_FullCycle_Stress(t *testing.T) {
	d := NewDispatcher()
	ready := make(chan struct{})
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			<-ready
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)

	const numCalls = 200
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()

			startDone := make(chan funcapi.AsyncStartResponse, 1)
			err := d.asyncStart.Handle(ctx, &funcapi.AsyncStartCmd{
				Task: runtime.Task{ID: registry.NewID("test", "func")},
			}, emitFunc(func(data any, _ error) {
				startDone <- data.(funcapi.AsyncStartResponse)
			}))
			require.NoError(t, err)
			startResult := <-startDone
			require.Nil(t, startResult.Error)

			ready <- struct{}{}

			awaitDone := make(chan funcapi.AsyncAwaitResponse, 1)
			err = d.asyncAwait.Handle(ctx, &funcapi.AsyncAwaitCmd{
				CallID: startResult.CallID,
			}, emitFunc(func(data any, _ error) {
				awaitDone <- data.(funcapi.AsyncAwaitResponse)
			}))
			require.NoError(t, err)
			awaitResult := <-awaitDone
			assert.Nil(t, awaitResult.Error)
			assert.False(t, awaitResult.Cancelled)
		}()
	}

	wg.Wait()
}

func BenchmarkAsyncStartHandler(b *testing.B) {
	d := NewDispatcher()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}
	emit := emitFunc(func(_ any, _ error) {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.asyncStart.Handle(ctx, cmd, emit)
	}
}

func BenchmarkAsyncStartHandler_Parallel(b *testing.B) {
	d := NewDispatcher()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := setupDispatcherTestContext(mock)
	cmd := &funcapi.AsyncStartCmd{
		Task: runtime.Task{ID: registry.NewID("test", "func")},
	}
	emit := emitFunc(func(_ any, _ error) {})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = d.asyncStart.Handle(ctx, cmd, emit)
		}
	})
}
