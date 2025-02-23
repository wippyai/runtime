package funcs

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"
	"testing"

	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/engine"
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

func (m *mockExecutor) Execute(runtime.Task) (chan *runtime.Result, error) {
	if m.err != nil {
		return nil, m.err
	}

	resultChan := make(chan *runtime.Result, 1)
	resultChan <- m.result
	close(resultChan)

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
	baseCtx := context.Background()
	baseCtx = context.WithValue(baseCtx, contextapi.TranscoderCtx, createTestTranscoder())

	t.Run("call with single argument", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("success"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function", "test_arg")
			assert(err == nil, "expected no error but got: " .. tostring(err))
			assert(result == "success", "expected 'success' but got: " .. tostring(result))
		`, "test_call")
		require.NoError(t, err)
	})

	t.Run("call with multiple arguments", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("multi_success"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function", "arg1", 42, {key = "value"})
			assert(err == nil, "expected no error but got: " .. tostring(err))
			assert(result == "multi_success")
		`, "test_call_multi")
		require.NoError(t, err)
	})

	t.Run("run returns only scheduling error", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("bg_success"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local err = executor.run("bg_function", "arg1", "arg2")
			assert(err == nil, "expected no error but got: " .. tostring(err))
		`, "test_run")
		require.NoError(t, err)
	})

	t.Run("run with executor error", func(t *testing.T) {
		mockExec := &mockExecutor{
			err: fmt.Errorf("scheduling error"),
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local err = executor.run("bg_function", "arg1")
			assert(err == "scheduling error", "expected 'scheduling error' but got: " .. tostring(err))
		`, "test_run_error")
		require.NoError(t, err)
	})

	t.Run("call without arguments", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("no_args"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function")
			assert(err == nil)
			assert(result == "no_args")
		`, "test_call_no_args")
		require.NoError(t, err)
	})

	t.Run("missing target name", func(t *testing.T) {
		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, &mockExecutor{})

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("")
			assert(result == nil)
			assert(err ~= nil)
		`, "test_missing_target")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target name is required")
	})
}

func TestExecutorModule_WithContext(t *testing.T) {
	logger := zap.NewNop()
	baseCtx := context.Background()
	baseCtx = context.WithValue(baseCtx, contextapi.TranscoderCtx, createTestTranscoder())

	t.Run("with valid context values", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("success"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.new():with_context({key = "value"}):call("test_function")
			assert(err == nil, "expected no error but got: " .. tostring(err))
			assert(result == "success", "expected 'success' but got: " .. tostring(result))
		`, "test_with_context")

		require.NoError(t, err)
	})

	t.Run("with invalid context keys", func(t *testing.T) {
		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, &mockExecutor{})

		err = vm.DoString(ctx, `
			local executor = require("executor")
			executor.new():with_context({[1] = "value"})
		`, "test_invalid_context_keys")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context keys must be strings")
	})
}

func TestExecutorModule_ContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	baseCtx := context.Background()
	baseCtx = context.WithValue(baseCtx, contextapi.TranscoderCtx, createTestTranscoder())

	t.Run("cancellation during execution", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("should not receive"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx, cancel := context.WithCancel(baseCtx)
		ctx = context.WithValue(ctx, contextapi.FunctionsCtx, mockExec)

		// Cancel context immediately after starting execution
		go func() {
			cancel()
		}()

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function")
			assert(err == "execution canceled", "expected cancellation error but got: " .. tostring(err))
			assert(result == nil, "expected nil result on cancellation")
		`, "test_context_cancellation")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})
}

func TestExecutorModule_ResultError(t *testing.T) {
	logger := zap.NewNop()
	baseCtx := context.Background()
	baseCtx = context.WithValue(baseCtx, contextapi.TranscoderCtx, createTestTranscoder())

	t.Run("executor returns error in result", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: nil,
				Error:   fmt.Errorf("execution failed"),
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function")
			assert(err == "execution failed", "expected error message but got: " .. tostring(err))
			assert(result == nil, "expected nil result on error")
		`, "test_result_error")
		require.NoError(t, err)
	})
}

func TestExecutorModule_MissingContext(t *testing.T) {
	logger := zap.NewNop()

	t.Run("nil context", func(t *testing.T) {
		mod := NewFunctionModule(context.Background())
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local executor = require("executor")
			local result, err = executor.call("test_function")
			assert(err == "no context found", "expected context error but got: " .. tostring(err))
			assert(result == nil, "expected nil result when context missing")
		`, "test_nil_context")
		require.Error(t, err)
	})

	t.Run("missing executor in context", func(t *testing.T) {
		mod := NewFunctionModule(context.Background())
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(context.Background(), contextapi.TranscoderCtx, createTestTranscoder())

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function")
			assert(err == "executor not found in context", "expected executor error but got: " .. tostring(err))
			assert(result == nil, "expected nil result when executor missing")
		`, "test_missing_executor")
		require.Error(t, err)
		require.ErrorContains(t, err, "executor not found in context")
	})

	t.Run("missing transcoder in context", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("test"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(context.Background())
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(context.Background(), contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function")
			assert(err == "transcoder not found in context", "expected transcoder error but got: " .. tostring(err))
			assert(result == nil, "expected nil result when transcoder missing")
		`, "test_missing_transcoder")
		require.Error(t, err)
		require.ErrorContains(t, err, "transcoder not found in context")
	})
}

func TestExecutorModule_NilPayload(t *testing.T) {
	logger := zap.NewNop()
	baseCtx := context.Background()
	baseCtx = context.WithValue(baseCtx, contextapi.TranscoderCtx, createTestTranscoder())

	t.Run("nil payload in result", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: nil,
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			local result, err = executor.call("test_function")
			assert(err == nil, "expected no error but got: " .. tostring(err))
			assert(result == nil, "expected nil result for nil payload")
		`, "test_nil_payload")
		require.NoError(t, err)
	})
}

func TestExecutorModule_InstanceIsolation(t *testing.T) {
	logger := zap.NewNop()
	baseCtx := context.Background()
	baseCtx = context.WithValue(baseCtx, contextapi.TranscoderCtx, createTestTranscoder())

	t.Run("separate instances have independent contexts", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("success"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			
			-- Spawn two separate executor instances
			local exec1 = executor.new()
			local exec2 = executor.new()
			
			-- Set different contexts for each instance
			exec1 = exec1:with_context({user = "user1", role = "admin"})
			exec2 = exec2:with_context({user = "user2", role = "user"})
			
			-- Both instances should work independently
			local result1, err1 = exec1:call("test_function")
			assert(err1 == nil, "exec1 call failed: " .. tostring(err1))
			
			local result2, err2 = exec2:call("test_function")
			assert(err2 == nil, "exec2 call failed: " .. tostring(err2))
			
			-- Modifying one instance shouldn't affect the other
			exec1 = exec1:with_context({user = "user1_modified", role = "super_admin"})
			
			-- exec2 should maintain its original context
			local result3, err3 = exec2:call("test_function")
			assert(err3 == nil, "exec2 second call failed: " .. tostring(err3))
		`, "test_instance_isolation")
		require.NoError(t, err)
	})

	t.Run("new instances start with empty context", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("success"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			
			-- Spawn an executor and set its context
			local exec1 = executor.new()
			exec1 = exec1:with_context({user = "user1"})
			
			-- Spawn a new executor - it should start with an empty context
			local exec2 = executor.new()
			
			-- Both should work but have different contexts
			local result1, err1 = exec1:call("test_function")
			assert(err1 == nil, "exec1 call failed: " .. tostring(err1))
			
			local result2, err2 = exec2:call("test_function")
			assert(err2 == nil, "exec2 call failed: " .. tostring(err2))
		`, "test_new_instance_empty_context")
		require.NoError(t, err)
	})

	t.Run("context updates preserve independence", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("success"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			
			-- Spawn an executor and set initial context
			local exec1 = executor.new()
			exec1 = exec1:with_context({value = "original"})
			
			-- Spawn a new instance with modified context
			local exec2 = exec1:with_context({value = "modified"})
			
			-- Both should work with their respective contexts
			local result1, err1 = exec1:call("test_function")
			assert(err1 == nil, "exec1 call failed: " .. tostring(err1))
			assert(result1 == "success", "unexpected result from exec1")
			
			local result2, err2 = exec2:call("test_function")
			assert(err2 == nil, "exec2 call failed: " .. tostring(err2))
			assert(result2 == "success", "unexpected result from exec2")
			
			-- Further modifications to exec2 shouldn't affect exec1
			exec2 = exec2:with_context({value = "modified_again"})
			
			-- Original exec1 should still work with its context
			local result3, err3 = exec1:call("test_function")
			assert(err3 == nil, "exec1 second call failed: " .. tostring(err3))
			assert(result3 == "success", "unexpected result from exec1 second call")
		`, "test_context_updates")
		require.NoError(t, err)
	})

	t.Run("nested context updates", func(t *testing.T) {
		mockExec := &mockExecutor{
			result: &runtime.Result{
				Payload: payload.New("success"),
				Error:   nil,
			},
		}

		mod := NewFunctionModule(baseCtx)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := context.WithValue(baseCtx, contextapi.FunctionsCtx, mockExec)

		err = vm.DoString(ctx, `
			local executor = require("executor")
			
			-- Spawn an executor and chain multiple context updates
			local funcs = executor.new()
			funcs = funcs:with_context({level = "1"})
			funcs = funcs:with_context({level = "2"})
			funcs = funcs:with_context({level = "3"})
			
			-- Verify the executor still works after multiple context updates
			local result, err = funcs:call("test_function")
			assert(err == nil, "call after multiple context updates failed: " .. tostring(err))
			assert(result == "success", "unexpected result after context updates")
		`, "test_nested_context_updates")
		require.NoError(t, err)
	})
}
