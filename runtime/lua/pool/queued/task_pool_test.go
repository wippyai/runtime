package queued

import (
	"context"
	"fmt"
	ctxapi "github.com/ponyruntime/pony/api/context"
	runtime2 "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// mockTranscoder is a simple implementation of payload.Transcoder for testing
type mockTranscoder struct{}

func (m *mockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	// If already in the right format, return as is
	if p.Format() == format {
		return p, nil
	}

	// Only support Lua format for testing
	if format == payload.Lua {
		// For testing, we'll just pass through the value
		luaValue, ok := p.Data().(lua.LValue)
		if !ok {
			// Convert string to lua string
			if str, ok := p.Data().(string); ok {
				return payload.NewPayload(lua.LString(str), payload.Lua), nil
			}
			// Convert int to lua number
			if num, ok := p.Data().(int); ok {
				return payload.NewPayload(lua.LNumber(num), payload.Lua), nil
			}
			return nil, fmt.Errorf("unsupported data type for transcoding: %T", p.Data())
		}
		return payload.NewPayload(luaValue, payload.Lua), nil
	}

	return nil, fmt.Errorf("unsupported format: %v", format)
}

// Implement Unmarshaler interface
func (m *mockTranscoder) Unmarshal(_ payload.Payload, _ interface{}) error {
	return fmt.Errorf("unmarshal not implemented for testing")
}

// setupTestContext creates a context with a mock transcoder for testing
func setupTestContext(ctx context.Context) context.Context {
	return payload.WithTranscoder(ctx, &mockTranscoder{})
}

// createTestTask creates a runtime.Task for testing
func createTestTask(id string, args ...interface{}) runtime.Task {
	// Convert args to payloads
	payloads := make(payload.Payloads, len(args))
	for i, arg := range args {
		// For Lua values, use Lua format
		if lv, ok := arg.(lua.LValue); ok {
			payloads[i] = payload.NewPayload(lv, payload.Lua)
		} else {
			// For other values, use Golang format
			payloads[i] = payload.NewPayload(arg, payload.Golang)
		}
	}

	return runtime.Task{
		Payloads: payloads,
	}
}

// waitForResult waits for a result from the given channel with timeout
func waitForResult(_ testing.TB, resultChan chan *runtime.Result, timeout time.Duration) (*runtime.Result, error) {
	select {
	case result, ok := <-resultChan:
		if !ok {
			return nil, fmt.Errorf("result channel closed unexpectedly")
		}
		return result, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for result")
	}
}

func TestTaskPool_Execute_Basic(t *testing.T) {
	f, err := setupTestFactory() // Use default config
	assert.NoError(t, err)

	p, err := NewTaskPool(f, "test", WithTaskSize(1), WithTaskWorkers(1), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), 5*time.Second)
	defer cancel()
	ctx = setupTestContext(ctx)

	task := createTestTask("test", lua.LString("hello"))
	resultChan, err := p.Execute(ctx, task)
	require.NoError(t, err)

	result, err := waitForResult(t, resultChan, 5*time.Second)
	require.NoError(t, err)
	require.NoError(t, result.Error)

	luaValue, ok := result.Value.Data().(lua.LValue)
	require.True(t, ok, "Expected lua value")
	assert.Equal(t, lua.LString("hello"), luaValue)
}

func TestTaskPool_Execute_AfterClose(t *testing.T) {
	f, err := setupTestFactory()
	require.NoError(t, err)

	p, err := NewTaskPool(f, "test", WithTaskSize(1), WithTaskWorkers(1), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)

	p.Close()

	ctx := setupTestContext(ctxapi.NewRootContext())
	task := createTestTask("test", lua.LNil)

	_, err = p.Execute(ctx, task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pool is closed")
}

func TestTaskPool_Execute_Failure(t *testing.T) {
	f, err := setupTestFactory(
		withFunction(`
			function fail()
				error("intentional failure")
			end
			return fail
		`, "fail"),
		withFunction(`
			function test(arg)
				return arg
			end
			return test
		`, "test"),
	)
	require.NoError(t, err)

	p, err := NewTaskPool(f, "fail", WithTaskSize(1), WithTaskWorkers(1), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), 5*time.Second)
	defer cancel()
	ctx = setupTestContext(ctx)

	// Run failing function
	task := createTestTask("fail", lua.LNil)
	resultChan, err := p.Execute(ctx, task)
	require.NoError(t, err)

	result, err := waitForResult(t, resultChan, 5*time.Second)
	require.NoError(t, err)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "intentional failure")

	// Create a new pool for the test method
	p2, err := NewTaskPool(f, "test", WithTaskSize(1), WithTaskWorkers(1), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p2.Close()

	// Verify pool still works
	task = createTestTask("test", lua.LString("test"))
	resultChan, err = p2.Execute(ctx, task)
	require.NoError(t, err)

	result, err = waitForResult(t, resultChan, 5*time.Second)
	require.NoError(t, err)
	require.NoError(t, result.Error)

	luaValue, ok := result.Value.Data().(lua.LValue)
	require.True(t, ok, "Expected lua value")
	assert.Equal(t, lua.LString("test"), luaValue)
}

func TestTaskPool_ParallelExecution(t *testing.T) {
	f, err := setupTestFactory() // Default function is sufficient for parallel test
	require.NoError(t, err)

	p, err := NewTaskPool(f, "test", WithTaskSize(3), WithTaskWorkers(3), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	var wg sync.WaitGroup
	results := make(chan string, 10)
	baseCtx := setupTestContext(ctxapi.NewRootContext())

	// Launch 10 jobs with 3 workers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
			defer cancel()

			task := createTestTask("test", lua.LString(fmt.Sprintf("job-%d", id)))
			resultChan, err := p.Execute(ctx, task)
			if err != nil {
				results <- fmt.Sprintf("error-%d", id)
				return
			}

			result, err := waitForResult(t, resultChan, 5*time.Second)
			if err != nil || result.Error != nil {
				results <- fmt.Sprintf("error-%d", id)
				return
			}

			luaValue, ok := result.Value.Data().(lua.LValue)
			if !ok {
				results <- fmt.Sprintf("error-%d", id)
				return
			}

			results <- luaValue.String()
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
}

func TestTaskPool_WorkerDistribution(t *testing.T) {
	f, err := setupTestFactory(
		withFunction(`
			local id = 0
			function get_id()
				id = id + 1
				return id
			end
			return get_id
		`, "get_id"),
	)
	require.NoError(t, err)

	p, err := NewTaskPool(f, "get_id", WithTaskSize(3), WithTaskWorkers(3), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	var wg sync.WaitGroup
	var mu sync.Mutex
	idCounts := make(map[string]int)
	baseCtx := setupTestContext(ctxapi.NewRootContext())

	// Run multiple tasks and track distribution
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
			defer cancel()

			task := createTestTask("get_id", lua.LNil)
			resultChan, err := p.Execute(ctx, task)
			if err != nil {
				return
			}

			result, err := waitForResult(t, resultChan, 5*time.Second)
			if err != nil || result.Error != nil {
				return
			}

			luaValue, ok := result.Value.Data().(lua.LValue)
			if !ok {
				return
			}

			mu.Lock()
			idCounts[luaValue.String()]++
			mu.Unlock()
		}()
	}

	wg.Wait()

	mu.Lock()
	uniqueWorkers := len(idCounts)
	mu.Unlock()

	assert.True(t, uniqueWorkers > 1, "Tasks should be distributed across multiple workers")
}

func TestTaskPool_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	t.Run("rapid parallel execution and close", func(t *testing.T) {
		iterations := 5
		for i := 0; i < iterations; i++ {
			f, err := setupTestFactory()
			require.NoError(t, err)

			p, err := NewTaskPool(f, "test", WithTaskSize(3), WithTaskWorkers(3), WithTaskLogger(zap.NewNop()))
			require.NoError(t, err)

			var wg sync.WaitGroup
			successCount := atomic.Int32{}
			baseCtx := setupTestContext(ctxapi.NewRootContext())

			// Launch many parallel jobs
			for j := 0; j < 1000; j++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()

					ctx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
					defer cancel()

					task := createTestTask("test", lua.LString(fmt.Sprintf("job-%d", id)))
					resultChan, err := p.Execute(ctx, task)
					if err != nil {
						return
					}

					result, err := waitForResult(t, resultChan, 2*time.Second)
					if err == nil && result != nil && result.Error == nil {
						successCount.Add(1)
					}
				}(j)

				if j == 500 {
					go p.Close()
				}
			}

			wg.Wait()
			success := successCount.Load()
			assert.True(t, success > 0, "Some jobs should succeed")
			assert.True(t, success < 1000, "Not all jobs should succeed due to close")
		}
	})
}

func TestTaskPool_QueueBehavior(t *testing.T) {
	f, err := setupTestFactory(
		withFunction(`
			function sleep()
				local time = require("time")
				time.sleep(time.parse_duration("100ms"))
				return "done"
			end
			return sleep
		`, "sleep"),
	)
	require.NoError(t, err)

	p, err := NewTaskPool(f, "sleep", WithTaskSize(1), WithTaskWorkers(1), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	var wg sync.WaitGroup
	results := make(chan string, 10)
	baseCtx := setupTestContext(ctxapi.NewRootContext())

	// Queue several jobs that will take time to complete
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
			defer cancel()

			task := createTestTask("sleep", lua.LNil)
			resultChan, err := p.Execute(ctx, task)
			if err != nil {
				results <- fmt.Sprintf("error-%d", id)
				return
			}

			result, err := waitForResult(t, resultChan, 5*time.Second)
			if err != nil || result.Error != nil {
				results <- fmt.Sprintf("error-%d", id)
				return
			}

			luaValue, ok := result.Value.Data().(lua.LValue)
			if !ok {
				results <- fmt.Sprintf("error-%d", id)
				return
			}

			results <- luaValue.String()
		}(i)
	}

	wg.Wait()
	close(results)

	// Verify all jobs completed
	var count int
	for range results {
		count++
	}
	require.Equal(t, 10, count, "All queued jobs should complete")
}

func BenchmarkTaskPool_Execute(b *testing.B) {
	f, err := setupTestFactory(
		withFunction(`
			function bench(arg)
				return arg
			end
			return bench
		`, "bench"),
	)
	require.NoError(b, err)

	workers := runtime2.GOMAXPROCS(0)
	p, err := NewTaskPool(f, "bench", WithTaskSize(workers), WithTaskWorkers(workers))
	require.NoError(b, err)
	defer p.Close()

	baseCtx := setupTestContext(ctxapi.NewRootContext())
	task := createTestTask("bench", lua.LString("benchmark"))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := logs.WithLogger(baseCtx, zap.NewNop())
			resultChan, err := p.Execute(ctx, task)
			if err != nil {
				b.Fatal(err)
			}

			result, err := waitForResult(b, resultChan, 1*time.Second)
			if err != nil {
				b.Fatal(err)
			}

			if result.Error != nil {
				b.Fatal(result.Error)
			}

			luaValue, ok := result.Value.Data().(lua.LValue)
			if !ok {
				b.Fatal("expected lua value")
			}

			if luaValue != lua.LString("benchmark") {
				b.Fatal("unexpected result")
			}
		}
	})
}
