package executor

import (
	"context"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

type mockExecutor struct {
	result *runtime.Result
	err    error
}

func (m *mockExecutor) Execute(task runtime.Task) (chan *runtime.Result, error) {
	if m.err != nil {
		return nil, m.err
	}

	resultChan := make(chan *runtime.Result, 1)
	resultChan <- m.result
	close(resultChan)

	return resultChan, nil
}

func TestExecutorModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("successful execution with argument", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.NewPayload("success", payload.Lua),
				Error:   nil,
			},
		}

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(context.Background(), contextapi.ExecutorCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function", "test_arg")
			assert(err == nil)
			assert(result == "success")
		`, "test_sync")
		require.NoError(t, err)
	})

	t.Run("execution with no args", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.NewPayload("no_args", payload.Lua),
				Error:   nil,
			},
		}

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(context.Background(), contextapi.ExecutorCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function")
			assert(err == nil)
			assert(result == "no_args")
		`, "test_no_args")
		require.NoError(t, err)
	})

	t.Run("execution with missing target", func(t *testing.T) {
		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(context.Background(), contextapi.ExecutorCtx, &mockExecutor{})

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("")
		`, "test_missing_target")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target name is required")
	})

	t.Run("execution with missing executor", func(t *testing.T) {
		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.Background()

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function")
		`, "test_missing_executor")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "executor not found in context")
	})

	t.Run("execution with executor error", func(t *testing.T) {
		mockExec := &mockExecutor{
			err: fmt.Errorf("executor error"),
		}

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(context.Background(), contextapi.ExecutorCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function")
			assert(result == nil)
			assert(err == "executor error")
		`, "test_executor_error")
		require.NoError(t, err)
	})

	t.Run("execution with result error", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Error: fmt.Errorf("result error"),
			},
		}

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(context.Background(), contextapi.ExecutorCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function")
			assert(result == nil)
			assert(err == "result error")
		`, "test_result_error")
		require.NoError(t, err)
	})
}
