package function

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/internal/uniqid"
)

func setupTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	return process.WithPIDGenerator(ctx, pidGen)
}

type mockRegistry struct {
	callFn func(context.Context, runtime.Task) (*runtime.Result, error)
}

func (m *mockRegistry) Call(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	if m.callFn != nil {
		return m.callFn(ctx, task)
	}
	return &runtime.Result{}, nil
}

func TestAsyncCallRegistry_Start(t *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := context.Background()
	id := reg.Start(ctx, mock, runtime.Task{ID: registry.NewID("test", "func")})

	assert.NotZero(t, id)
}

func TestAsyncCallRegistry_Await(t *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("async result")}, nil
		},
	}

	ctx := context.Background()
	id := reg.Start(ctx, mock, runtime.Task{ID: registry.NewID("test", "func")})

	result, err := reg.Await(ctx, id)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAsyncCallRegistry_AwaitNotFound(t *testing.T) {
	reg := NewAsyncCallRegistry()
	ctx := context.Background()

	_, err := reg.Await(ctx, 999)
	assert.ErrorIs(t, err, function.ErrCallNotFound)
}

func TestAsyncCallRegistry_Cancel(t *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx := context.Background()
	id := reg.Start(ctx, mock, runtime.Task{ID: registry.NewID("test", "func")})

	err := reg.Cancel(id)
	require.NoError(t, err)

	_, err = reg.Await(ctx, id)
	assert.ErrorIs(t, err, function.ErrCallCancelled)
}

func TestAsyncCallRegistry_CancelNotFound(t *testing.T) {
	reg := NewAsyncCallRegistry()

	err := reg.Cancel(999)
	assert.ErrorIs(t, err, function.ErrCallNotFound)
}

func TestAsyncCallRegistry_Close(t *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx := context.Background()
	reg.Start(ctx, mock, runtime.Task{ID: registry.NewID("test", "func1")})
	reg.Start(ctx, mock, runtime.Task{ID: registry.NewID("test", "func2")})

	reg.Close()

	reg.mu.Lock()
	count := len(reg.calls)
	reg.mu.Unlock()

	assert.Zero(t, count)
}

func TestAsyncCallRegistry_ContextGet(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	r1 := GetAsyncCallRegistry(ctx)
	assert.Nil(t, r1)

	r2 := NewAsyncCallRegistry()
	err := SetAsyncCallRegistry(ctx, r2)
	assert.NoError(t, err)

	r3 := GetAsyncCallRegistry(ctx)
	assert.Equal(t, r2, r3)
}

func TestAsyncCallRegistry_ContextNoFrame(t *testing.T) {
	ctx := context.Background()

	r := GetAsyncCallRegistry(ctx)
	assert.Nil(t, r)

	err := SetAsyncCallRegistry(ctx, NewAsyncCallRegistry())
	assert.Error(t, err)
}

func TestAsyncCallRegistry_Concurrent(t *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New(task.ID.String())}, nil
		},
	}

	ctx := context.Background()
	const numCalls = 100

	var wg sync.WaitGroup
	ids := make([]uint64, numCalls)

	for i := 0; i < numCalls; i++ {
		ids[i] = reg.Start(ctx, mock, runtime.Task{
			ID: registry.NewID("test", fmt.Sprintf("func%d", i)),
		})
	}

	wg.Add(numCalls)
	for i := 0; i < numCalls; i++ {
		go func(idx int) {
			defer wg.Done()
			result, err := reg.Await(ctx, ids[idx])
			assert.NoError(t, err)
			assert.NotNil(t, result)
		}(i)
	}

	wg.Wait()
}

func TestAsyncCallRegistry_Error(t *testing.T) {
	reg := NewAsyncCallRegistry()
	expectedErr := errors.New("function error")
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return nil, expectedErr
		},
	}

	ctx := context.Background()
	id := reg.Start(ctx, mock, runtime.Task{ID: registry.NewID("test", "func")})

	_, err := reg.Await(ctx, id)
	assert.ErrorIs(t, err, expectedErr)
}

func TestAsyncCallRegistry_ResultError(t *testing.T) {
	reg := NewAsyncCallRegistry()
	expectedErr := errors.New("result error")
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Error: expectedErr}, nil
		},
	}

	ctx := context.Background()
	id := reg.Start(ctx, mock, runtime.Task{ID: registry.NewID("test", "func")})

	_, err := reg.Await(ctx, id)
	assert.ErrorIs(t, err, expectedErr)
}

func TestAsyncCallRegistry_AwaitContextCancel(t *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			time.Sleep(1 * time.Second)
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}

	ctx := context.Background()
	id := reg.Start(ctx, mock, runtime.Task{ID: registry.NewID("test", "func")})

	awaitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := reg.Await(awaitCtx, id)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestCallAsync(t *testing.T) {
	executor, bus := setupTest()
	ctx := setupTestContext()
	require.NoError(t, executor.Start(ctx))
	defer func() { _ = executor.Stop() }()

	funcID := registry.NewID("test", "async_func")
	called := false

	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
				called = true
				return &runtime.Result{Value: payload.New("async result")}, nil
			},
		},
	})

	time.Sleep(10 * time.Millisecond)

	ch, err := executor.CallAsync(ctx, runtime.Task{ID: funcID})
	require.NoError(t, err)

	result := <-ch
	ReleaseResultChan(ch)

	require.NoError(t, result.Error)
	assert.True(t, called)
	assert.NotNil(t, result.Result)
}

func TestCallAsync_NotFound(t *testing.T) {
	executor, _ := setupTest()
	ctx := setupTestContext()
	require.NoError(t, executor.Start(ctx))
	defer func() { _ = executor.Stop() }()

	_, err := executor.CallAsync(ctx, runtime.Task{
		ID: registry.NewID("test", "nonexistent"),
	})
	assert.Error(t, err)
}

func BenchmarkAsyncCallRegistry_Start(b *testing.B) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}
	ctx := context.Background()
	task := runtime.Task{ID: registry.NewID("test", "func")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := reg.Start(ctx, mock, task)
		_, _ = reg.Await(ctx, id)
	}
}

func BenchmarkAsyncCallRegistry_StartParallel(b *testing.B) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}
	ctx := context.Background()
	task := runtime.Task{ID: registry.NewID("test", "func")}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := reg.Start(ctx, mock, task)
			_, _ = reg.Await(ctx, id)
		}
	})
}

func BenchmarkCallAsync(b *testing.B) {
	executor, bus := setupTest()
	ctx := setupTestContext()
	_ = executor.Start(ctx)
	defer func() { _ = executor.Stop() }()

	funcID := registry.NewID("test", "bench_func")
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
				return &runtime.Result{Value: payload.New("result")}, nil
			},
		},
	})
	time.Sleep(10 * time.Millisecond)

	task := runtime.Task{ID: funcID}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := executor.CallAsync(ctx, task)
		<-ch
		ReleaseResultChan(ch)
	}
}

func BenchmarkCallAsync_Parallel(b *testing.B) {
	executor, bus := setupTest()
	ctx := setupTestContext()
	_ = executor.Start(ctx)
	defer func() { _ = executor.Stop() }()

	funcID := registry.NewID("test", "bench_func")
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
				return &runtime.Result{Value: payload.New("result")}, nil
			},
		},
	})
	time.Sleep(10 * time.Millisecond)

	task := runtime.Task{ID: funcID}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ch, _ := executor.CallAsync(ctx, task)
			<-ch
			ReleaseResultChan(ch)
		}
	})
}

func BenchmarkCallAsyncCallback(b *testing.B) {
	executor, bus := setupTest()
	ctx := setupTestContext()
	_ = executor.Start(ctx)
	defer func() { _ = executor.Stop() }()

	funcID := registry.NewID("test", "bench_func")
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
				return &runtime.Result{Value: payload.New("result")}, nil
			},
		},
	})
	time.Sleep(10 * time.Millisecond)

	task := runtime.Task{ID: funcID}
	done := make(chan struct{}, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = executor.CallAsyncCallback(ctx, task, func(_ *runtime.Result, _ error) {
			done <- struct{}{}
		})
		<-done
	}
}

func BenchmarkCallAsyncCallback_Parallel(b *testing.B) {
	executor, bus := setupTest()
	ctx := setupTestContext()
	_ = executor.Start(ctx)
	defer func() { _ = executor.Stop() }()

	funcID := registry.NewID("test", "bench_func")
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
				return &runtime.Result{Value: payload.New("result")}, nil
			},
		},
	})
	time.Sleep(10 * time.Millisecond)

	task := runtime.Task{ID: funcID}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		done := make(chan struct{}, 1)
		for pb.Next() {
			_ = executor.CallAsyncCallback(ctx, task, func(_ *runtime.Result, _ error) {
				done <- struct{}{}
			})
			<-done
		}
	})
}

// BenchmarkCall benchmarks the synchronous Call path (used by HTTP handlers)
func BenchmarkCall(b *testing.B) {
	executor, bus := setupTest()
	ctx := setupTestContext()
	_ = executor.Start(ctx)
	defer func() { _ = executor.Stop() }()

	funcID := registry.NewID("test", "bench_func")
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
				return &runtime.Result{Value: payload.New("result")}, nil
			},
		},
	})
	time.Sleep(10 * time.Millisecond)

	task := runtime.Task{ID: funcID}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Call(ctx, task)
	}
}

func BenchmarkCall_Parallel(b *testing.B) {
	executor, bus := setupTest()
	ctx := setupTestContext()
	_ = executor.Start(ctx)
	defer func() { _ = executor.Stop() }()

	funcID := registry.NewID("test", "bench_func")
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
				return &runtime.Result{Value: payload.New("result")}, nil
			},
		},
	})
	time.Sleep(10 * time.Millisecond)

	task := runtime.Task{ID: funcID}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = executor.Call(ctx, task)
		}
	})
}

// Stress tests

func TestAsyncCallRegistry_StressTest(t *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}
	ctx := context.Background()

	const numCalls = 1000
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func(idx int) {
			defer wg.Done()
			id := reg.Start(ctx, mock, runtime.Task{
				ID: registry.NewID("test", fmt.Sprintf("func%d", idx)),
			})
			result, err := reg.Await(ctx, id)
			assert.NoError(t, err)
			assert.NotNil(t, result)
		}(i)
	}

	wg.Wait()
}

func TestAsyncCallRegistry_StressCancelWhileRunning(t *testing.T) {
	reg := NewAsyncCallRegistry()
	var started sync.WaitGroup
	started.Add(100)

	mock := &mockRegistry{
		callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			started.Done()
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ctx := context.Background()

	const numCalls = 100
	ids := make([]uint64, numCalls)

	for i := 0; i < numCalls; i++ {
		ids[i] = reg.Start(ctx, mock, runtime.Task{
			ID: registry.NewID("test", fmt.Sprintf("func%d", i)),
		})
	}

	started.Wait()

	var wg sync.WaitGroup
	wg.Add(numCalls)
	for i := 0; i < numCalls; i++ {
		go func(idx int) {
			defer wg.Done()
			_ = reg.Cancel(ids[idx])
		}(i)
	}
	wg.Wait()

	for i := 0; i < numCalls; i++ {
		_, err := reg.Await(ctx, ids[i])
		if err != nil && !errors.Is(err, function.ErrCallCancelled) && !errors.Is(err, function.ErrCallNotFound) {
			t.Errorf("unexpected error for call %d: %v", i, err)
		}
	}
}

func TestAsyncCallRegistry_StressMixedOperations(_ *testing.T) {
	reg := NewAsyncCallRegistry()
	mock := &mockRegistry{
		callFn: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
			time.Sleep(time.Microsecond)
			return &runtime.Result{Value: payload.New("result")}, nil
		},
	}
	ctx := context.Background()

	const numOperations = 500
	var wg sync.WaitGroup
	wg.Add(numOperations * 3)

	for i := 0; i < numOperations; i++ {
		go func(idx int) {
			defer wg.Done()
			id := reg.Start(ctx, mock, runtime.Task{
				ID: registry.NewID("test", fmt.Sprintf("start%d", idx)),
			})
			_, _ = reg.Await(ctx, id)
		}(i)

		go func(idx int) {
			defer wg.Done()
			id := reg.Start(ctx, mock, runtime.Task{
				ID: registry.NewID("test", fmt.Sprintf("cancel%d", idx)),
			})
			_ = reg.Cancel(id)
			_, _ = reg.Await(ctx, id)
		}(i)

		go func(idx int) {
			defer wg.Done()
			//nolint:gosec // test code, safe conversion
			_, _ = reg.Await(ctx, uint64(idx+1000000))
		}(i)
	}

	wg.Wait()
}

func TestCallAsync_StressParallel(t *testing.T) {
	executor, bus := setupTest()
	ctx := setupTestContext()
	require.NoError(t, executor.Start(ctx))
	defer func() { _ = executor.Stop() }()

	funcID := registry.NewID("test", "stress_func")
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
				return &runtime.Result{Value: payload.New("result")}, nil
			},
		},
	})
	time.Sleep(10 * time.Millisecond)

	const numCalls = 500
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()
			ch, err := executor.CallAsync(ctx, runtime.Task{ID: funcID})
			if err != nil {
				return
			}
			result := <-ch
			ReleaseResultChan(ch)
			assert.NoError(t, result.Error)
			assert.NotNil(t, result.Result)
		}()
	}

	wg.Wait()
}

func TestReleaseResultChan_SafeDoubleRelease(t *testing.T) {
	ch := resultChanPool.Get().(chan *CallResult)
	ch <- &CallResult{Result: &runtime.Result{Value: payload.New("test")}}

	<-ch
	ReleaseResultChan(ch)

	ch2 := resultChanPool.Get().(chan *CallResult)
	assert.NotNil(t, ch2)

	ReleaseResultChan(ch2)
}

func TestReleaseResultChan_EmptyChannel(t *testing.T) {
	ch := resultChanPool.Get().(chan *CallResult)
	ReleaseResultChan(ch)

	ch2 := resultChanPool.Get().(chan *CallResult)
	assert.NotNil(t, ch2)
	ReleaseResultChan(ch2)
}
