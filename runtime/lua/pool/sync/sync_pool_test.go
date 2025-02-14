package sync

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/factory"
	timemod "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	factoryOpts []factory.Option
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
func withEngineOption(opt engine.Option) testOption {
	return func(cfg *testFactoryConfig) {
		cfg.engineOpts = append(cfg.engineOpts, opt)
	}
}

// withFactoryOption adds a factory option to the configuration
func withFactoryOption(opt factory.Option) testOption {
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

	factoryOpts := append(cfg.factoryOpts,
		factory.WithEngineOption(engine.WithGlobalValue("_VERSION", lua.LString("test"))),
	)

	// all rest of the functions
	for _, fn := range cfg.functions[1:] {

		factoryOpts = append(cfg.factoryOpts,
			factory.WithEngineOption(engine.WithFunctionProto(fn.name, fn.proto)),
		)
	}

	// Prepare engine options
	engineOpts := cfg.engineOpts
	for _, opt := range engineOpts {
		factoryOpts = append(factoryOpts, factory.WithEngineOption(opt))
	}

	f, err := factory.NewRunnerFactory(logger, compiled, factoryOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory: %w", err)
	}

	return f, nil
}

func TestPool_Execute_Basic(t *testing.T) {
	f, err := setupTestFactory() // Use default config
	assert.NoError(t, err)

	p, err := NewPool(f, WithSize(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	arg := lua.LString("hello")
	result, err := p.Execute(ctx, "test", arg)
	require.NoError(t, err)
	assert.Equal(t, arg, result)
}

func TestPool_Execute_AfterClose(t *testing.T) {
	f, err := setupTestFactory()
	require.NoError(t, err)

	p, err := NewPool(f, WithSize(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)

	p.Close()

	_, err = p.Execute(context.Background(), "test", lua.LNil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pool is closed")
}

func TestPool_Execute_ContextCancellation(t *testing.T) {
	f, err := setupTestFactory()
	require.NoError(t, err)

	p, err := NewPool(f, WithSize(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = p.Execute(ctx, "test", lua.LNil)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "context canceled")
}

func TestPool_Execute_Failure(t *testing.T) {
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

	p, err := NewPool(f, WithSize(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run failing function
	_, err = p.Execute(ctx, "fail", lua.LNil)
	assert.Error(t, err)

	// Verify pool still works
	result, err := p.Execute(ctx, "test", lua.LString("test"))
	assert.NoError(t, err)
	assert.Equal(t, lua.LString("test"), result)
}

func TestPool_Close(t *testing.T) {
	f, err := setupTestFactory()
	require.NoError(t, err)

	p, err := NewPool(f, WithSize(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)

	p.Close()

	_, err = p.Execute(context.Background(), "test", lua.LNil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pool is closed")
}

func TestPool_ParallelExecution(t *testing.T) {
	f, err := setupTestFactory() // Default function is sufficient for parallel test
	require.NoError(t, err)

	p, err := NewPool(f, WithSize(3), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	var wg sync.WaitGroup
	results := make(chan string, 10)

	// Launch 10 jobs with 3 VMs
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

// //	func TestPool_JobCompletionOnClose(t *testing.T) {
// //		f := setupTestFactory(t,
// //			withFunction(`
// //				function sleep_test()
// //					local time = require("time")
// //					time.sleep(time.parse_duration("1s"))
// //					return "completed"
// //				end
// //				return sleep_test
// //			`, "sleep_test", nil),
// //		)
// //
// //		p, err := NewPool(f, WithSize(1), WithLogger(zap.NewNop()))
// //		require.NoError(t, err)
// //
// //		// Launch a job that will run for a known duration
// //		resultChan := make(chan lua.LValue, 1)
// //		errorChan := make(chan error, 1)
// //
// //		// Launch the job
// //		go func() {
// //			result, err := p.Execute(context.Background(), "sleep_test", lua.LNil)
// //			if err != nil {
// //				errorChan <- err
// //				return
// //			}
// //			resultChan <- result
// //		}()
// //
// //		// Give job time to start
// //		time.Sleep(10 * time.Millisecond)
// //
// //		// Stop the pool while job is running
// //		p.Close()
// //
// //		// wait for result or error
// //		select {
// //		case result := <-resultChan:
// //			require.Equal(t, lua.LString("completed"), result, "Job should complete")
// //		case err := <-errorChan:
// //			t.Fatalf("Job failed: %v", err)
// //		case <-time.After(2 * time.Second):
// //			t.Fatal("Job did not complete in time")
// //		}
// //	}
func TestPool_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	t.Run("rapid parallel execution and close", func(t *testing.T) {
		iterations := 5
		for i := 0; i < iterations; i++ {
			f, err := setupTestFactory()
			require.NoError(t, err)

			p, err := NewPool(f, WithSize(3), WithLogger(zap.NewNop()))
			require.NoError(t, err)

			var wg sync.WaitGroup
			successCount := atomic.Int32{}

			// Launch many parallel jobs
			for j := 0; j < 1000; j++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					result, err := p.Execute(context.Background(), "test",
						lua.LString(fmt.Sprintf("job-%d", id)))
					if err == nil && result != nil {
						successCount.Add(1)
					}
				}(j)

				if j == 500 {
					go p.Close()
				}
			}

			wg.Wait()
			success := successCount.Load()
			require.True(t, success > 0, "Some jobs should succeed")
			require.True(t, success < 1000, "Not all jobs should succeed due to close")
		}
	})
}

func TestPool_VMReuse(t *testing.T) {
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

	p, err := NewPool(f, WithSize(1), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	defer p.Close()

	// Run multiple times - should get incrementing IDs from same VM
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
}

func BenchmarkPool_Execute(b *testing.B) {

	f, err := setupTestFactory(
		withFunction(`
			function bench(arg)
				return arg
			end
			return bench
		`, "bench"),
	)
	require.NoError(b, err)

	p, err := NewPool(f, WithSize(runtime.GOMAXPROCS(0)))
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
