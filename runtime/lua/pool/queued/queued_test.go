package queued

import (
	"context"
	"fmt"
	ctxapi "github.com/wippyai/runtime/api/context"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"github.com/wippyai/runtime/runtime/lua/engine"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
	"go.uber.org/zap"
)

// testFunction represents a Lua function definition for testing
type testFunction struct {
	source string
	name   string
	proto  *lua.FunctionProto
}

// testFactoryConfig holds configuration for test factory setup
type testFactoryConfig struct {
	functions   []testFunction
	engineOpts  []engine.Option
	factoryOpts []component.Option
}

// testOption configures the test factory
type testOption func(*testFactoryConfig)

// withFunction adds a test function to the configuration
func withFunction(source string, method string) testOption {
	return func(cfg *testFactoryConfig) {
		cfg.functions = append(cfg.functions, testFunction{
			source: source,
			name:   method,
		})
	}
}

// withEngineOption adds an engine option to the configuration
//
//nolint:unused // to be used in tests
func withEngineOption(opt engine.Option) testOption {
	return func(cfg *testFactoryConfig) {
		cfg.engineOpts = append(cfg.engineOpts, opt)
	}
}

// withFactoryOption adds a factory option to the configuration
//
//nolint:unused // to be used in tests
func withFactoryOption(opt component.Option) testOption {
	return func(cfg *testFactoryConfig) {
		cfg.factoryOpts = append(cfg.factoryOpts, opt)
	}
}

// defaultTestFunction returns the basic echo test function
func defaultTestFunction() testFunction {
	return testFunction{
		source: `
			function test(arg)
				return arg
			end
			return test
		`,
		name: "test",
	}
}

// setupTestFactory creates a configured factory for testing with the given options
func setupTestFactory(opts ...testOption) (api.Factory, error) {
	logger := zap.NewNop()

	// Initialize config with defaults
	cfg := &testFactoryConfig{
		functions: []testFunction{defaultTestFunction()},
	}

	// Apply options
	for _, opt := range opts {
		opt(cfg)
	}

	// compile all funcs
	for i, fn := range cfg.functions {
		chunk, err := parse.Parse(strings.NewReader(fn.source), fn.name)
		if err != nil {
			return nil, fmt.Errorf("failed to parse function %q: %w", fn.name, err)
		}

		proto, err := lua.Compile(chunk, fn.name)
		if err != nil {
			return nil, fmt.Errorf("failed to compile function %q: %w", fn.name, err)
		}

		cfg.functions[i].proto = proto
	}

	// Spawn compiled main
	compiled := &code.CompiledMain{
		Main:     cfg.functions[0].proto,
		FuncName: cfg.functions[0].name,
		Dependencies: []code.CompiledProto{
			{
				Name: "time",
				Node: &code.Node{
					Kind:   api.KindModule,
					Module: timemod.NewTimeModule(),
				},
			},
		},
	}

	//nolint:gocritic // new array used only for comparison
	factoryOpts := append(cfg.factoryOpts,
		component.WithEngineOption(engine.WithGlobalValue("_VERSION", lua.LString("test"))),
	)

	// all rest of the functions
	for _, fn := range cfg.functions[1:] {
		factoryOpts = append(factoryOpts,
			component.WithEngineOption(engine.WithFunctionProto(fn.name, fn.proto)),
		)
	}

	// Prepare engine options
	engineOpts := cfg.engineOpts
	for _, opt := range engineOpts {
		factoryOpts = append(factoryOpts, component.WithEngineOption(opt))
	}

	f, err := component.NewRunnerFactory(logger, compiled, factoryOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory: %w", err)
	}

	return f, nil
}

func TestQueuedPool_Execute_Basic(t *testing.T) {
	f, err := setupTestFactory() // Use default config
	assert.NoError(t, err)

	p, err := NewPool(f, WithSize(1), WithWorkers(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), 5*time.Second)
	defer cancel()

	arg := lua.LString("hello")
	result, err := p.Execute(ctx, "test", arg)
	require.NoError(t, err)
	assert.Equal(t, arg, result)
}

func TestQueuedPool_Execute_AfterClose(t *testing.T) {
	f, err := setupTestFactory()
	require.NoError(t, err)

	p, err := NewPool(f, WithSize(1), WithWorkers(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)

	p.Close()

	_, err = p.Execute(context.Background(), "test", lua.LNil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pool is closed")
}

func TestQueuedPool_Execute_ContextCancellation(t *testing.T) {
	f, err := setupTestFactory()
	require.NoError(t, err)

	p, err := NewPool(f, WithSize(1), WithWorkers(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
	cancel() // Cancel immediately

	_, err = p.Execute(ctx, "test", lua.LNil)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "context canceled")
}

func TestQueuedPool_Execute_Failure(t *testing.T) {
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

	p, err := NewPool(f, WithSize(1), WithWorkers(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), 5*time.Second)
	defer cancel()

	// Run failing function
	_, err = p.Execute(ctx, "fail", lua.LNil)
	assert.Error(t, err)

	// Verify pool still works
	result, err := p.Execute(ctx, "test", lua.LString("test"))
	assert.NoError(t, err)
	assert.Equal(t, lua.LString("test"), result)
}

func TestQueuedPool_ParallelExecution(t *testing.T) {
	f, err := setupTestFactory() // Default function is sufficient for parallel test
	require.NoError(t, err)

	p, err := NewPool(f, WithSize(3), WithWorkers(3), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	var wg sync.WaitGroup
	results := make(chan string, 10)

	// Launch 10 jobs with 3 workers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			result, err := p.Execute(context.Background(), "test",
				lua.LString(fmt.Sprintf("job-%d", id)))
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
}

func TestQueuedPool_WorkerDistribution(t *testing.T) {
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

	p, err := NewPool(f, WithSize(3), WithWorkers(3), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	var wg sync.WaitGroup
	var mu sync.Mutex
	idCounts := make(map[string]int)

	// Run multiple tasks and track distribution
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := p.Execute(context.Background(), "get_id", lua.LNil)
			if err != nil {
				return
			}
			mu.Lock()
			idCounts[result.String()]++
			mu.Unlock()
		}()
	}

	wg.Wait()

	mu.Lock()
	uniqueWorkers := len(idCounts)
	mu.Unlock()

	assert.True(t, uniqueWorkers > 1, "Tasks should be distributed across multiple workers")
}

func TestQueuedPool_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	t.Run("rapid parallel execution and close", func(t *testing.T) {
		iterations := 5
		for i := 0; i < iterations; i++ {
			f, err := setupTestFactory()
			require.NoError(t, err)

			p, err := NewPool(f, WithSize(3), WithWorkers(3), WithLogger(zap.NewNop()))
			require.NoError(t, err)

			var wg sync.WaitGroup
			var successCount atomic.Int32

			// Ensure some jobs complete before closing
			const totalJobs = 1000
			const jobsBeforeClose = 500

			// Launch a few jobs and ensure they complete before closing
			for j := 0; j < 10; j++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					result, err := p.Execute(context.Background(), "test",
						lua.LString(fmt.Sprintf("guaranteed-job-%d", id)))
					if err == nil && result != nil {
						successCount.Add(1)
					}
				}(j)
			}

			// Wait for these initial jobs to complete
			wg.Wait()

			// Reset wait group for the main batch
			var mainWg sync.WaitGroup

			// Launch the main batch of parallel jobs
			for j := 0; j < totalJobs; j++ {
				mainWg.Add(1)
				go func(id int) {
					defer mainWg.Done()
					result, err := p.Execute(context.Background(), "test",
						lua.LString(fmt.Sprintf("job-%d", id)))
					if err == nil && result != nil {
						successCount.Add(1)
					}
				}(j)

				if j == jobsBeforeClose {
					go p.Close()
				}
			}

			mainWg.Wait()
			success := successCount.Load()
			require.True(t, success > 0, "Some jobs should succeed")
			require.True(t, success < 1000, "Not all jobs should succeed due to close")
		}
	})
}

func TestQueuedPool_QueueBehavior(t *testing.T) {
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

	p, err := NewPool(f, WithSize(1), WithWorkers(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	var wg sync.WaitGroup
	results := make(chan string, 10)

	// Queue several jobs that will take time to complete
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), 5*time.Second)
			defer cancel()

			result, err := p.Execute(ctx, "sleep", lua.LNil)
			if err != nil {
				results <- fmt.Sprintf("error-%d", id)
				return
			}
			results <- result.String()
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

func BenchmarkQueuedPool_Execute(b *testing.B) {
	f, err := setupTestFactory(
		withFunction(`
			function bench(arg)
				return arg
			end
			return bench
		`, "bench"),
	)
	require.NoError(b, err)

	workers := runtime.GOMAXPROCS(0)
	p, err := NewPool(f, WithSize(workers), WithWorkers(workers))
	require.NoError(b, err)
	defer p.Close()

	ctx := ctxapi.NewRootContext()
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
