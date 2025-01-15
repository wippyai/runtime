package queued

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/factory"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func setupTestPool(t *testing.T, size, workers int) *Pool {
	logger := zap.NewNop()
	f := factory.NewFactory(logger)

	// Add test functions to VM config
	factory.WithFunction("test", `
		function test(arg)
			return arg
		end
		return test
	`)(f)

	factory.WithFunction("block", `
		function block()
			while true do
			end
			return nil
		end
		return block
	`)(f)

	factory.WithFunction("fail", `
		function fail()
			error("intentional failure")
		end
		return fail
	`)(f)

	factory.WithFunction("get_id", `
		local id = 0
		function get_id()
			id = id + 1
			return id
		end
		return get_id
	`)(f)

	// Create pool with custom size and workers
	p, err := NewPool(f,
		WithSize(size),
		WithWorkers(workers),
		WithLogger(logger),
	)
	require.NoError(t, err)

	return p
}

func TestQueuedPool_Execute(t *testing.T) {
	t.Run("basic execution", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		p := setupTestPool(t, 1, 1)
		defer p.Close()

		arg := lua.LString("hello")
		result, err := p.Execute(ctx, "test", arg)
		require.NoError(t, err)
		assert.Equal(t, arg, result)
	})

	t.Run("execution after close", func(t *testing.T) {
		p := setupTestPool(t, 1, 1)
		p.Close()

		_, err := p.Execute(context.Background(), "test", lua.LNil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pool is closed")
	})

	t.Run("context cancellation", func(t *testing.T) {
		p := setupTestPool(t, 1, 1)
		defer p.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := p.Execute(ctx, "test", lua.LNil)
		assert.Error(t, err)
		assert.ErrorContains(t, err, "context canceled")
	})

	t.Run("failed execution", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		p := setupTestPool(t, 1, 1)
		defer p.Close()

		// Execute failing function
		_, err := p.Execute(ctx, "fail", lua.LNil)
		assert.Error(t, err)

		// Verify pool still works
		result, err := p.Execute(ctx, "test", lua.LString("test"))
		assert.NoError(t, err)
		assert.Equal(t, lua.LString("test"), result)
	})
}

func TestQueuedPool_ParallelExecution(t *testing.T) {
	t.Run("execute multiple jobs with multiple workers", func(t *testing.T) {
		p := setupTestPool(t, 3, 3) // Pool with 3 VMs and 3 workers
		defer p.Close()

		var wg sync.WaitGroup
		results := make(chan string, 10)

		// Launch 10 jobs
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				result, err := p.Execute(context.Background(), "test", lua.LString(fmt.Sprintf("job-%d", id)))
				if err != nil {
					results <- fmt.Sprintf("error-%d", id)
					return
				}
				results <- result.String()
			}(i)
		}

		wg.Wait()
		close(results)

		// Collect and verify results
		var count int
		for r := range results {
			require.Contains(t, r, "job-")
			count++
		}
		require.Equal(t, 10, count, "All jobs should complete")
	})

	t.Run("verify worker distribution", func(t *testing.T) {
		p := setupTestPool(t, 3, 3)
		defer p.Close()

		var wg sync.WaitGroup
		executed := make(map[string]int)
		var mu sync.Mutex

		// Execute multiple tasks and track results
		for i := 0; i < 30; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := p.Execute(context.Background(), "get_id", lua.LNil)
				if err != nil {
					return
				}
				mu.Lock()
				executed[result.String()]++
				mu.Unlock()
			}()
		}

		wg.Wait()

		// Verify distribution
		mu.Lock()
		defer mu.Unlock()
		workerCount := len(executed)
		assert.True(t, workerCount > 1, "Tasks should be distributed across workers (got %d workers)", workerCount)
	})
}

func TestQueuedPool_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	t.Run("rapid parallel execution with queue", func(t *testing.T) {
		p := setupTestPool(t, 3, 3)
		var wg sync.WaitGroup
		successCount := atomic.Int32{}

		// Launch many parallel jobs
		for j := 0; j < 100; j++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()

				result, err := p.Execute(ctx, "test", lua.LString(fmt.Sprintf("job-%d", id)))
				if err == nil && result != nil {
					successCount.Add(1)
				}
			}(j)
		}

		wg.Wait()
		p.Close()

		success := successCount.Load()
		require.True(t, success > 0, "Some jobs should succeed")
	})
}

func TestQueuedPool_QueueBehavior(t *testing.T) {
	t.Run("queue overflow handling", func(t *testing.T) {
		// Create a pool with small number of workers but larger queue
		p := setupTestPool(t, 1, 1)
		defer p.Close()

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()

				_, _ = p.Execute(ctx, "test", lua.LString(fmt.Sprintf("job-%d", id)))
			}(i)
		}

		wg.Wait()
	})
}

func BenchmarkQueuedPool_Execute(b *testing.B) {
	logger := zap.NewNop()
	vmConfig := factory.NewFactory(logger)
	factory.WithFunction("bench", `
		function test(arg)
			return arg
		end
		return test
	`)(vmConfig)

	workers := runtime.GOMAXPROCS(0)
	p, err := NewPool(vmConfig,
		WithSize(workers),
		WithWorkers(workers))
	require.NoError(b, err)
	defer p.Close()

	ctx := context.Background()
	arg := lua.LString("benchmark")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result, err := p.Execute(ctx, "bench", arg)
			if err != nil {
				b.Fatal(err)
			}
			if result != arg {
				b.Fatal("unexpected result")
			}
		}
	})
}
