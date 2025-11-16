package flex

import (
	"context"
	"fmt"
	runtime2 "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"

	api "github.com/wippyai/runtime/api/runtime/lua"
	luaconv "github.com/wippyai/runtime/system/payload/lua"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Import test helper functions from other packages
// These are used in other pool tests so we're maintaining consistency

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
		return luaconv.ExportPayload(luaValue), nil
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

// executeWithTimeout executes a task with timeout
func executeWithTimeout(ctx context.Context, p *TaskPool, task runtime.Task, timeout time.Duration) (*runtime.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return p.Execute(ctx, task)
}

// TestFactory is a mock factory for testing
type TestFactory struct {
	createVMCalls int32
	shouldFail    bool
}

func (f *TestFactory) Compile() error {
	return nil
}

// CreateVM implements the Factory interface
func (f *TestFactory) CreateVM() (api.VM, error) {
	atomic.AddInt32(&f.createVMCalls, 1)

	if f.shouldFail {
		return nil, fmt.Errorf("factory failure")
	}

	return &TestVM{
		closed: false,
	}, nil
}

// TestVM is a mock VM for testing
type TestVM struct {
	closed     bool
	stateValue int32
}

// Execute implements the VM interface
func (vm *TestVM) Execute(_ context.Context, method string, args ...lua.LValue) (lua.LValue, error) {
	if vm.closed {
		return nil, fmt.Errorf("VM is closed")
	}

	switch method {
	case "test":
		if len(args) > 0 {
			return args[0], nil
		}
		return lua.LNil, nil
	case "fail":
		return nil, fmt.Errorf("intentional failure")
	case "get_id":
		// Each VM has its own state counter
		vm.stateValue++
		return lua.LNumber(vm.stateValue), nil
	case "sleep":
		time.Sleep(100 * time.Millisecond)
		return lua.LString("done"), nil
	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}
}

// Close implements the VM interface
func (vm *TestVM) Close() {
	vm.closed = true
}

// TestFlexPool_Execute_Basic tests basic execution
func TestFlexPool_Execute_Basic(t *testing.T) {
	factory := &TestFactory{}

	p, err := NewTaskPool(factory, "test", WithTaskMaxSize(10), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), 5*time.Second)
	defer cancel()
	ctx = setupTestContext(ctx)

	task := createTestTask("test", lua.LString("hello"))
	result, err := executeWithTimeout(ctx, p, task, 5*time.Second)
	require.NoError(t, err)
	require.NoError(t, result.Error)

	luaValue, ok := result.Value.Data().(lua.LValue)
	require.True(t, ok, "Expected lua value")
	assert.Equal(t, lua.LString("hello"), luaValue)

	// Verify that a VM was created
	assert.Equal(t, int32(1), factory.createVMCalls)
}

// TestFlexPool_Execute_AfterClose tests that execution fails after close
func TestFlexPool_Execute_AfterClose(t *testing.T) {
	factory := &TestFactory{}

	p, err := NewTaskPool(factory, "test", WithTaskMaxSize(10), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)

	p.Close()

	ctx := setupTestContext(ctxapi.NewRootContext())
	task := createTestTask("test", lua.LNil)

	_, err = p.Execute(ctx, task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pool is closed")
}

// TestFlexPool_Execute_FactoryFailure tests handling of factory failures
func TestFlexPool_Execute_FactoryFailure(t *testing.T) {
	factory := &TestFactory{shouldFail: true}

	p, err := NewTaskPool(factory, "test", WithTaskMaxSize(10), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx := setupTestContext(ctxapi.NewRootContext())
	task := createTestTask("test", lua.LNil)

	_, err = executeWithTimeout(ctx, p, task, 5*time.Second)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "factory failure")
}

// TestFlexPool_Execute_VMFailure tests handling of VM execution failures
func TestFlexPool_Execute_VMFailure(t *testing.T) {
	factory := &TestFactory{}

	p, err := NewTaskPool(factory, "fail", WithTaskMaxSize(10), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx := setupTestContext(ctxapi.NewRootContext())
	task := createTestTask("fail", lua.LNil)

	_, err = executeWithTimeout(ctx, p, task, 5*time.Second)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "intentional failure")
}

// TestFlexPool_ParallelExecution tests parallel execution
func TestFlexPool_ParallelExecution(t *testing.T) {
	factory := &TestFactory{}

	// Set a lower max size to test concurrency limits
	p, err := NewTaskPool(factory, "test", WithTaskMaxSize(3), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	var wg sync.WaitGroup
	results := make(chan string, 10)
	baseCtx := setupTestContext(ctxapi.NewRootContext())

	// Launch 10 jobs with max 3 concurrent
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
			defer cancel()

			task := createTestTask("test", lua.LString(fmt.Sprintf("job-%d", id)))
			result, err := executeWithTimeout(ctx, p, task, 5*time.Second)
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

	// Verify VM creation count
	assert.Equal(t, int32(10), factory.createVMCalls, "Each task should create its own VM")
}

// TestFlexPool_VMNotReused verifies that VMs are not reused
func TestFlexPool_VMNotReused(t *testing.T) {
	factory := &TestFactory{}

	p, err := NewTaskPool(factory, "get_id", WithTaskMaxSize(1), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx := setupTestContext(ctxapi.NewRootContext())

	// Run multiple times - should get incrementing IDs from same VM
	for i := 0; i < 5; i++ {
		task := createTestTask("get_id", lua.LNil)
		result, err := executeWithTimeout(ctx, p, task, 5*time.Second)
		require.NoError(t, err)
		require.NoError(t, result.Error)

		luaValue, ok := result.Value.Data().(lua.LValue)
		require.True(t, ok, "Expected lua value")

		id := float64(luaValue.(lua.LNumber))
		assert.Equal(t, float64(1), id, "Each VM should start with ID 1 since they're not reused")
	}

	// Verify we created 5 VMs
	assert.Equal(t, int32(5), factory.createVMCalls)
}

// TestFlexPool_Concurrency tests that concurrency is limited by maxSize
func TestFlexPool_Concurrency(t *testing.T) {
	factory := &TestFactory{}

	// Only allow 2 concurrent executions
	p, err := NewTaskPool(factory, "sleep", WithTaskMaxSize(2), WithTaskLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx := setupTestContext(ctxapi.NewRootContext())

	// Serve time
	start := time.Now()

	// Launch 6 jobs that each take about 100ms
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			task := createTestTask("sleep", lua.LNil)
			result, err := p.Execute(ctx, task)
			if err != nil {
				t.Logf("Error executing task: %v", err)
				return
			}
			if result.Error != nil {
				t.Logf("Task execution error: %v", result.Error)
				return
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// With 6 tasks of 100ms each and max concurrency of 2,
	// we expect execution to take at least 300ms (6/2 * 100ms)
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(300),
		"Execution should be limited by maxSize")
}

// BenchmarkFlexPool_Execute benchmarks task execution
func BenchmarkFlexPool_Execute(b *testing.B) {
	factory := &TestFactory{}

	maxProcs := runtime2.GOMAXPROCS(0)
	p, err := NewTaskPool(factory, "test", WithTaskMaxSize(maxProcs*2))
	require.NoError(b, err)
	defer p.Close()

	baseCtx := setupTestContext(ctxapi.NewRootContext())
	task := createTestTask("test", lua.LString("benchmark"))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := logs.WithLogger(baseCtx, zap.NewNop())
			result, err := p.Execute(ctx, task)
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
