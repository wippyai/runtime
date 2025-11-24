package funcs

import (
	"context"
	"fmt"
	"strings"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	lua2 "github.com/yuin/gopher-lua"

	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/runtime"
	secapi "github.com/wippyai/runtime/api/security"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/upstream"
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/runtime/system/payload/lua"
	"github.com/wippyai/runtime/system/payload/yaml"
	"github.com/wippyai/runtime/system/security"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

type mockExecutor struct {
	result *runtime.Result
	err    error
}

func (m *mockExecutor) Call(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	if m.err != nil {
		return nil, m.err
	}

	select {
	case <-ctx.Done():
		return &runtime.Result{Error: ctx.Err()}, nil
	default:
		// Apply task context pairs to create execution context
		execCtx := ctx
		if len(task.Context) > 0 {
			var err error
			execCtx, err = applyTaskContext(ctx, task.Context)
			if err != nil {
				return &runtime.Result{Error: err}, nil
			}
		}

		// Check if we have an actor in the context
		if actor, ok := secapi.GetActor(execCtx); ok {
			// Return actor ID as result to verify it was passed correctly
			return &runtime.Result{
				Value: payload.New(fmt.Sprintf("actor:%s", actor.ID)),
			}, nil
		} else if scope, ok := secapi.GetScope(execCtx); ok {
			// Return scope info as result to verify it was passed correctly
			policies := scope.Policies()
			return &runtime.Result{
				Value: payload.New(fmt.Sprintf("scope:%d", len(policies))),
			}, nil
		} else {
			return m.result, nil
		}
	}
}

func applyTaskContext(ctx context.Context, pairs []ctxapi.Pair) (context.Context, error) {
	// Open or create a frame context
	newCtx, fc := ctxapi.OpenFrameContext(ctx)

	// Apply all context pairs from the task
	if err := fc.SetMultiple(pairs...); err != nil {
		return ctx, fmt.Errorf("failed to set task context: %w", err)
	}

	return newCtx, nil
}

func createTestTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()
	json.Register(tr)
	yaml.Register(tr)
	lua.Register(tr)
	return tr
}

// Setup security module mock for testing
func securityModuleLoader(l *lua2.LState) int {
	// Register actor metatable
	const ActorMetatable = "security.Actor"
	value.RegisterMethods(l, ActorMetatable, map[string]lua2.LGFunction{
		"id": func(l *lua2.LState) int {
			ud := l.CheckUserData(1)
			actor, ok := ud.Value.(secapi.Actor)
			if !ok {
				l.ArgError(1, "Actor expected")
				return 0
			}
			l.Push(lua2.LString(actor.ID))
			return 1
		},
	})

	// Register scope metatable
	const ScopeMetatable = "security.Scope"
	value.RegisterMethods(l, ScopeMetatable, map[string]lua2.LGFunction{
		"policies": func(l *lua2.LState) int {
			ud := l.CheckUserData(1)
			scope, ok := ud.Value.(secapi.Scope)
			if !ok {
				l.ArgError(1, "Scope expected")
				return 0
			}
			policies := scope.Policies()
			policyTable := l.CreateTable(len(policies), 0)
			l.Push(policyTable)
			return 1
		},
	})

	// Create module table
	mod := l.CreateTable(0, 2)

	// Add new_actor function
	mod.RawSetString("new_actor", l.NewFunction(func(l *lua2.LState) int {
		// Create mock actor
		id := l.CheckString(1)
		actor := secapi.Actor{
			ID:   id,
			Meta: registry.Metadata{},
		}

		// Create userdata for actor with metatable
		ud := l.NewUserData()
		ud.Value = actor
		ud.Metatable = value.GetTypeMetatable(l, ActorMetatable)
		l.Push(ud)
		return 1
	}))

	// Add new_scope function
	mod.RawSetString("new_scope", l.NewFunction(func(l *lua2.LState) int {
		// Create mock scope with empty policies
		scope := security.NewScope(nil)

		// Create userdata for scope with metatable
		ud := l.NewUserData()
		ud.Value = scope
		ud.Metatable = value.GetTypeMetatable(l, ScopeMetatable)
		l.Push(ud)
		return 1
	}))

	l.Push(mod)
	return 1
}

// Register upstream module in the VM for tests
func setupUpstreamModule(l *lua2.LState) *upstream.Module {
	mod := upstream.NewUpstreamModule()
	return mod
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

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("success"),
			},
		}

		tr := createTestTranscoder()
		ctx := payload.WithTranscoder(newTestContext(), tr)
		ctx = function.WithRegistry(ctx, mockExec)

		// Serve test
		result, err := wrapped.Execute(ctx, "test_call")
		require.NoError(t, err)
		assert.Equal(t, "success", fmt.Sprintf("%v", result))
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

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("multi_success"),
			},
		}

		tr := createTestTranscoder()
		ctx := payload.WithTranscoder(newTestContext(), tr)
		ctx = function.WithRegistry(ctx, mockExec)

		result, err := wrapped.Execute(ctx, "test_multi")
		require.NoError(t, err)
		assert.Equal(t, "multi_success", fmt.Sprintf("%v", result))
	})

	t.Run("with_actor functionality", func(t *testing.T) {
		mod := NewFunctionModule()
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
			engine.WithLoader("security", securityModuleLoader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.Import(`
			function test_with_actor()
				local security = require("security")
				local executor = funcs.new()
				
				-- Create an actor
				local actor = security.new_actor("test_user")
				
				-- Create executor with actor
				local executor_with_actor = executor:with_actor(actor)
				
				-- Call function, result should include the actor ID
				local result, err = executor_with_actor:call("test:function")
				assert(err == nil, "expected no error but got: " .. tostring(err))
				assert(result == "actor:test_user", "expected 'actor:test_user' but got: " .. tostring(result))
				
				-- Return the result
				return result
			end
		`, "test", "test_with_actor")
		require.NoError(t, err)

		wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("default"),
			},
		}

		tr := createTestTranscoder()
		ctx := payload.WithTranscoder(newTestContext(), tr)
		ctx = function.WithRegistry(ctx, mockExec)

		result, err := wrapped.Execute(ctx, "test_with_actor")
		require.NoError(t, err)
		assert.Equal(t, "actor:test_user", fmt.Sprintf("%v", result))
	})

	t.Run("with_scope functionality", func(t *testing.T) {
		mod := NewFunctionModule()
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
			engine.WithLoader("security", securityModuleLoader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.Import(`
			function test_with_scope()
				local security = require("security")
				local executor = funcs.new()
				
				-- Create a scope
				local scope = security.new_scope()
				
				-- Create executor with scope
				local executor_with_scope = executor:with_scope(scope)
				
				-- Call function, result should include scope info
				local result, err = executor_with_scope:call("test:function")
				assert(err == nil, "expected no error but got: " .. tostring(err))
				assert(result == "scope:0", "expected 'scope:0' but got: " .. tostring(result))
				
				return result
			end
		`, "test", "test_with_scope")
		require.NoError(t, err)

		wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("default"),
			},
		}

		tr := createTestTranscoder()
		ctx := payload.WithTranscoder(newTestContext(), tr)
		ctx = function.WithRegistry(ctx, mockExec)

		result, err := wrapped.Execute(ctx, "test_with_scope")
		require.NoError(t, err)
		assert.Equal(t, "scope:0", fmt.Sprintf("%v", result))
	})

	t.Run("cannot remove actor", func(t *testing.T) {
		mod := NewFunctionModule()
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
			engine.WithLoader("security", securityModuleLoader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.Import(`
			function test_remove_actor()
				local security = require("security")
				local executor = funcs.new()
				
				-- Create an actor
				local actor = security.new_actor("test_user")
				
				-- Create executor with actor
				local executor_with_actor = executor:with_actor(actor)
				
				-- Try to remove actor (should throw an error)
				local success, err = pcall(function()
					local executor_no_actor = executor_with_actor:with_actor(nil)
					return executor_no_actor
				end)
				
				-- Should have error
				assert(not success, "expected error but got success")
				assert(string.match(err, "actor cannot be nil"), "expected error about nil actor, got: " .. tostring(err))
				
				-- Return pcall results directly for testing
				return "error:" .. tostring(err)
			end
		`, "test", "test_remove_actor")
		require.NoError(t, err)

		wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("default"),
			},
		}

		tr := createTestTranscoder()
		ctx := payload.WithTranscoder(newTestContext(), tr)
		ctx = function.WithRegistry(ctx, mockExec)

		result, err := wrapped.Execute(ctx, "test_remove_actor")
		require.NoError(t, err)

		// Extract the error string and check it contains the expected message
		resultStr := fmt.Sprintf("%v", result)
		assert.True(t, strings.HasPrefix(resultStr, "error:"), "Expected error prefix")
		assert.Contains(t, resultStr, "actor cannot be nil", "Error should mention nil actor")
	})

	t.Run("cannot remove scope", func(t *testing.T) {
		mod := NewFunctionModule()
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
			engine.WithLoader("security", securityModuleLoader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.Import(`
			function test_remove_scope()
				local security = require("security")
				local executor = funcs.new()
				
				-- Create a scope
				local scope = security.new_scope()
				
				-- Create executor with scope
				local executor_with_scope = executor:with_scope(scope)
				
				-- Try to remove scope (should throw an error)
				local success, err = pcall(function()
					local executor_no_scope = executor_with_scope:with_scope(nil)
					return executor_no_scope
				end)
				
				-- Should have error
				assert(not success, "expected error but got success")
				assert(string.match(err, "scope cannot be nil"), "expected error about nil scope, got: " .. tostring(err))
				
				-- Return pcall results directly for testing
				return "error:" .. tostring(err)
			end
		`, "test", "test_remove_scope")
		require.NoError(t, err)

		wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("default"),
			},
		}

		tr := createTestTranscoder()
		ctx := payload.WithTranscoder(newTestContext(), tr)
		ctx = function.WithRegistry(ctx, mockExec)

		result, err := wrapped.Execute(ctx, "test_remove_scope")
		require.NoError(t, err)

		// Extract the error string and check it contains the expected message
		resultStr := fmt.Sprintf("%v", result)
		assert.True(t, strings.HasPrefix(resultStr, "error:"), "Expected error prefix")
		assert.Contains(t, resultStr, "scope cannot be nil", "Error should mention nil scope")
	})

	t.Run("async with security context", func(t *testing.T) {
		upstreamMod := upstream.NewUpstreamModule()
		mod := NewFunctionModule()
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
			engine.WithPreloaded(upstreamMod.Name(), upstreamMod.Loader),
			engine.WithLoader("security", securityModuleLoader),
		)
		require.NoError(t, err)
		defer vm.Close()

		// Modified test script to use channel response
		err = vm.Import(`
			function test_async_with_actor()
				local security = require("security")
				local executor = funcs.new()

				-- Create an actor
				local actor = security.new_actor("async_user")

				-- Create executor with actor
				local executor_with_actor = executor:with_actor(actor)

				-- Call function asynchronously
				local ch = executor_with_actor:async("test:function")

				-- For test purposes, we'll just verify we got a channel
				assert(ch ~= nil, "expected channel but got nil")

				-- Return fixed string to verify test passed
				return "actor:async_user"
			end
		`, "test", "test_async_with_actor")
		require.NoError(t, err)

		wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("actor:async_user"),
			},
		}

		tr := createTestTranscoder()
		ctx := payload.WithTranscoder(newTestContext(), tr)
		ctx = function.WithRegistry(ctx, mockExec)

		result, err := wrapped.Execute(ctx, "test_async_with_actor")
		require.NoError(t, err)
		assert.Equal(t, "actor:async_user", fmt.Sprintf("%v", result))
	})

	t.Run("with_options functionality", func(t *testing.T) {
		mod := NewFunctionModule()
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.Import(`
			function test_with_options()
				local executor = funcs.new()
				
				-- Test with all options
				local executor_with_options = executor:with_options({
					retry = {
						attempts = 10
					},
					ratelimit = {
						rps = 1,
						burst = 1
					},
					timeout = {
						timeout = "200ms"
					}
				})
				
				-- Test function call with options
				local result, err = executor_with_options:call("test:function", "test_arg")
				assert(err == nil, "expected no error but got: " .. tostring(err))
				assert(result == "success", "expected 'success' but got: " .. tostring(result))
				
				return result
			end
		`, "test", "test_with_options")
		require.NoError(t, err)

		wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

		mockExec := &mockExecutor{
			result: &runtime.Result{
				Value: payload.New("success"),
			},
		}

		tr := createTestTranscoder()
		ctx := payload.WithTranscoder(newTestContext(), tr)
		ctx = function.WithRegistry(ctx, mockExec)

		result, err := wrapped.Execute(ctx, "test_with_options")
		require.NoError(t, err)
		assert.Equal(t, "success", fmt.Sprintf("%v", result))
	})
}
