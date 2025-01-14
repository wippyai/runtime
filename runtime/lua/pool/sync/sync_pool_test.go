package sync

import (
	"context"
	"fmt"
	time2 "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cfg "github.com/ponyruntime/pony/runtime/lua/engine/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func setupTestPool(t *testing.T, size int) *Pool {
	logger := zap.NewNop()
	vmConfig := cfg.NewVMConfig(logger)
	cfg.WithModule(time2.NewTimeModule())(vmConfig)

	// Add a test function to the VM config
	cfg.WithFunction("test", `
        function test(arg)
            return arg
        end
        return test
    `)(vmConfig)

	// Add a blocking function that will sleep
	cfg.WithFunction("block", `
        function block()
            while true do
            end
            return nil
        end
        return block
    `)(vmConfig)

	// Add a function that will fail
	cfg.WithFunction("fail", `
        function fail()
            error("intentional failure")
        end
        return fail
    `)(vmConfig)

	// Add function that returns unique VM identifier
	cfg.WithFunction("get_id", `
        local id = 0
        function get_id()
            id = id + 1
            return id
        end
        return get_id
    `)(vmConfig)

	// Add a sleep function to VM config
	cfg.WithFunction("sleep_test", `
		   function test()
               local time = require("time") 
               time.sleep(time.parse_duration("1s"))
		       return "completed"
		   end
		   return test
		`)(vmConfig)

	// Create pool with custom size
	p, err := NewPool(vmConfig, WithSize(size), WithLogger(logger))
	require.NoError(t, err)

	return p
}

func TestPool_Execute(t *testing.T) {
	t.Run("basic execution", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		p := setupTestPool(t, 1)
		defer p.Close()

		arg := lua.LString("hello")
		result, err := p.Execute(ctx, "test", arg)
		require.NoError(t, err)
		assert.Equal(t, arg, result)
	})

	t.Run("execution after close", func(t *testing.T) {
		p := setupTestPool(t, 1)
		p.Close()

		_, err := p.Execute(context.Background(), "test", lua.LNil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pool is closed")
	})

	t.Run("context cancellation", func(t *testing.T) {
		p := setupTestPool(t, 1)
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

		p := setupTestPool(t, 1)
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

func TestPool_Close(t *testing.T) {
	t.Run("close and verify", func(t *testing.T) {
		p := setupTestPool(t, 1)
		p.Close()

		_, err := p.Execute(context.Background(), "test", lua.LNil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pool is closed")
	})
}

func TestPool_ParallelExecution(t *testing.T) {
	t.Run("execute multiple jobs in parallel", func(t *testing.T) {
		p := setupTestPool(t, 3) // Pool with 3 VMs
		defer p.Close()

		var wg sync.WaitGroup
		results := make(chan string, 10)

		// Launch 10 jobs with 3 VMs
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
}

func TestPool_JobCompletionOnClose(t *testing.T) {
	t.Run("ensure running job completes on close", func(t *testing.T) {
		p := setupTestPool(t, 1)

		// Start a job that will run for a known duration
		resultChan := make(chan lua.LValue, 1)
		errorChan := make(chan error, 1)

		// Start the job
		go func() {
			result, err := p.Execute(context.Background(), "sleep_test", lua.LNil)
			if err != nil {
				errorChan <- err
				return
			}
			resultChan <- result
		}()

		// Give job time to start
		time.Sleep(10 * time.Millisecond)

		// Close the pool while job is running
		p.Close()

		// Wait for result or error
		select {
		case result := <-resultChan:
			require.Equal(t, lua.LString("completed"), result, "Job should complete")
		case err := <-errorChan:
			t.Fatalf("Job failed: %v", err)
		case <-time.After(2 * time.Second):
			t.Fatal("Job did not complete in time")
		}
	})
}

func TestPool_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	t.Run("rapid parallel execution and close", func(t *testing.T) {
		iterations := 5
		for i := 0; i < iterations; i++ {
			p := setupTestPool(t, 3)
			var wg sync.WaitGroup
			successCount := atomic.Int32{}

			// Launch many parallel jobs
			for j := 0; j < 100; j++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					result, err := p.Execute(context.Background(), "test", lua.LString(fmt.Sprintf("job-%d", id)))
					if err == nil && result != nil {
						successCount.Add(1)
					}
				}(j)

				// Randomly close the pool while jobs are running
				if j == 50 {
					go p.Close()
				}
			}

			wg.Wait()
			success := successCount.Load()
			require.True(t, success > 0, "Some jobs should succeed")
			require.True(t, success < 100, "Not all jobs should succeed due to close")
		}
	})
}

func TestPool_VMReuse(t *testing.T) {
	t.Run("verify VM reuse", func(t *testing.T) {
		p := setupTestPool(t, 1) // Single VM pool
		defer p.Close()

		// Execute multiple times - should get incrementing IDs from same VM
		var lastID float64
		for i := 0; i < 5; i++ {
			result, err := p.Execute(context.Background(), "get_id", lua.LNil)
			require.NoError(t, err)

			id := float64(result.(lua.LNumber))
			if i > 0 {
				require.Equal(t, lastID+1, id, "IDs should increment indicating VM reuse")
			}
			lastID = id
		}
	})
}

func BenchmarkPool_Execute(b *testing.B) {
	logger := zap.NewNop()
	vmConfig := cfg.NewVMConfig(logger)
	cfg.WithFunction("bench", `
		function test(arg)
			return arg
		end
		return test
	`)(vmConfig)

	p, err := NewPool(vmConfig, WithSize(runtime.GOMAXPROCS(0)))
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
