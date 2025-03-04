package funcs

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/runtime"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockExecutor struct {
	result *runtime.Result
	err    error
}

func (m *mockExecutor) Call(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
	if m.err != nil {
		return nil, m.err
	}

	resultChan := make(chan *runtime.Result, 1)
	go func() {
		select {
		case <-ctx.Done():
			resultChan <- &runtime.Result{Error: ctx.Err()}
		default:
			resultChan <- m.result
		}
		close(resultChan)
	}()

	return resultChan, nil
}

func createTestTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()
	json.Register(tr)
	yaml.Register(tr)
	lua.Register(tr)
	return tr
}

func TestExecutorModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("call with single argument", func(t *testing.T) {
		// Create module first to get the loader
		mod := NewFunctionModule()

		// Create VM with the module preloaded
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.Import(`
			function test_call()
				local executor = funcs.new()
				local result, err = executor:call("test:function", "test_arg")
				assert(err == nil, "expected no error but got: " .. tostring(err))
				assert(result == "success", "expected 'success' but got: " .. tostring(result))
				return result
			end
		`, "test", "test_call")
		require.NoError(t, err)

		// Setup test environment
		wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

		// Create context with dependencies
		uw, ctx := wrapped.InitUnitOfWork(context.Background())
		defer func() { _ = uw.Close() }()

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("success"),
			},
		}

		tr := createTestTranscoder()
		ctx = payload.WithTranscoder(ctx, tr)
		ctx = function.WithFunctions(ctx, mockExec)

		// Start test
		result, err := wrapped.Execute(ctx, "test_call")
		require.NoError(t, err)
		assert.Equal(t, "success", result.String())
	})

	t.Run("call with multiple arguments", func(t *testing.T) {
		mod := NewFunctionModule()
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.Import(`
			function test_multi()
				local executor = funcs.new()
				local result, err = executor:call("test:function", "arg1", 42, {key = "value"})
				assert(err == nil, "expected no error but got: " .. tostring(err))
				assert(result == "multi_success", "expected 'multi_success' but got: " .. tostring(result))
				return result
			end
		`, "test", "test_multi")
		require.NoError(t, err)

		wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

		uw, ctx := wrapped.InitUnitOfWork(context.Background())
		defer func() { _ = uw.Close() }()

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("multi_success"),
			},
		}

		tr := createTestTranscoder()
		ctx = payload.WithTranscoder(ctx, tr)
		ctx = function.WithFunctions(ctx, mockExec)

		result, err := wrapped.Execute(ctx, "test_multi")
		require.NoError(t, err)
		assert.Equal(t, "multi_success", result.String())
	})

	t.Run("call with executor error", func(t *testing.T) {
		mod := NewFunctionModule()
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.Import(`
			function test_error()
				local executor = funcs.new()
				local result, err = executor:call("test:function")
				assert(err == "execution failed", "expected 'execution failed' but got: " .. tostring(err))
				assert(result == nil, "expected nil result on error")
				return "ok"
			end
		`, "test", "test_error")
		require.NoError(t, err)

		wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

		uw, ctx := wrapped.InitUnitOfWork(context.Background())
		defer func() { _ = uw.Close() }()

		mockExec := &mockExecutor{
			err: fmt.Errorf("execution failed"),
		}

		tr := createTestTranscoder()
		ctx = payload.WithTranscoder(ctx, tr)
		ctx = function.WithFunctions(ctx, mockExec)

		result, err := wrapped.Execute(ctx, "test_error")
		require.NoError(t, err)
		assert.Equal(t, "ok", result.String())
	})

	t.Run("context cancellation", func(t *testing.T) {
		mod := NewFunctionModule()
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.Import(`
			function test_cancel()
				local executor = funcs.new()
				local result, err = executor:call("test:function")
				assert(err == "context canceled", "expected context canceled error")
				assert(result == nil, "expected nil result on cancellation")
				return result
			end
		`, "test", "test_cancel")
		require.NoError(t, err)

		wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

		ctx, cancel := context.WithCancel(context.Background())

		uw, ctx := wrapped.InitUnitOfWork(context.Background())
		defer func() { _ = uw.Close() }()

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("should not receive"),
			},
		}

		tr := createTestTranscoder()
		ctx = payload.WithTranscoder(ctx, tr)
		ctx = function.WithFunctions(ctx, mockExec)

		// Cancel context during execution
		go func() {
			cancel()
		}()

		_, err = wrapped.Execute(ctx, "test_cancel")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	// todo: add async test
}
