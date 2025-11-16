package command

import (
	"context"
	"fmt"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestNewCommand(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := newTestContext()
	uw, ctx := engine.NewUnitOfWork(ctx, vm.State())

	t.Run("creates command with valid parameters", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil, payload.NewPayload("test", payload.Lua))

		assert.NotNil(t, cmd)
		assert.Contains(t, cmd.ID(), "cmd.test.")
		assert.Equal(t, runtime.Type("test"), cmd.Type())
		assert.Len(t, cmd.Params(), 1)
		assert.NotNil(t, cmd.responseChannel)
		assert.NotNil(t, cmd.channelValue)
		assert.Equal(t, uw, cmd.unitOfWork)
		assert.Nil(t, cmd.onCancel)
	})

	t.Run("creates command with multiple parameters", func(t *testing.T) {
		vm.State().SetContext(ctx)

		params := []payload.Payload{
			payload.NewPayload("param1", payload.Lua),
			payload.NewPayload(123, payload.Lua),
			payload.NewPayload(true, payload.Lua),
		}

		cmd := NewCommand(vm.State(), "multi", nil, params...)

		assert.NotNil(t, cmd)
		assert.Len(t, cmd.Params(), 3)
		assert.Equal(t, params, cmd.Params())
	})

	t.Run("creates command with cancellation handler", func(t *testing.T) {
		vm.State().SetContext(ctx)

		var cancelCalled bool
		onCancel := func(_ runtime.Command) {
			cancelCalled = true
		}

		cmd := NewCommand(vm.State(), "cancelable", onCancel)

		assert.NotNil(t, cmd)
		assert.NotNil(t, cmd.onCancel)

		cmd.onCancel(cmd)
		assert.True(t, cancelCalled)
	})

	t.Run("raises error when no unit of work context", func(t *testing.T) {
		// Create a new state without unit of work context
		state := lua.NewState()
		defer state.Close()

		// This should panic with "no unit of work context found"
		defer func() {
			if r := recover(); r != nil {
				switch v := r.(type) {
				case *lua.ApiError:
					switch obj := v.Object.(type) {
					case error:
						assert.Contains(t, obj.Error(), "no unit of work context found")
					case fmt.Stringer:
						assert.Contains(t, obj.String(), "no unit of work context found")
					default:
						assert.Contains(t, fmt.Sprintf("%v", obj), "no unit of work context found")
					}
				case error:
					assert.Contains(t, v.Error(), "no unit of work context found")
				case string:
					assert.Contains(t, v, "no unit of work context found")
				default:
					t.Errorf("unexpected panic type: %T", v)
				}
			} else {
				t.Error("expected panic but none occurred")
			}
		}()
		NewCommand(state, "test", nil)
	})
}

func TestCommand_Complete(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := newTestContext()
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	t.Run("completes command successfully", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		result := &runtime.Result{
			Value: payload.NewPayload("success", payload.Lua),
			Error: nil,
		}

		err := cmd.Complete(result)
		assert.NoError(t, err)
		assert.True(t, cmd.isCompleted())
		assert.False(t, cmd.isCanceled())
		assert.Equal(t, result, cmd.Result())
	})

	t.Run("completes command with error", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		result := &runtime.Result{
			Value: nil,
			Error: assert.AnError,
		}

		err := cmd.Complete(result)
		assert.NoError(t, err)
		assert.True(t, cmd.isCompleted())
		assert.False(t, cmd.isCanceled())
		assert.Equal(t, result, cmd.Result())
	})

	t.Run("returns error when already completed", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		result1 := &runtime.Result{Value: payload.NewPayload("first", payload.Lua)}
		result2 := &runtime.Result{Value: payload.NewPayload("second", payload.Lua)}

		err := cmd.Complete(result1)
		assert.NoError(t, err)

		err = cmd.Complete(result2)
		assert.Error(t, err)
		assert.Equal(t, ErrCommandCompleted, err)
		assert.Equal(t, result1, cmd.Result())
	})

	t.Run("returns error when already canceled", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		err := cmd.Cancel()
		assert.NoError(t, err)

		result := &runtime.Result{Value: payload.NewPayload("test", payload.Lua)}
		err = cmd.Complete(result)
		assert.Error(t, err)
		assert.Equal(t, ErrCommandCompleted, err)
	})
}

func TestCommand_Cancel(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := newTestContext()
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	t.Run("cancels command successfully", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		err := cmd.Cancel()
		assert.NoError(t, err)
		assert.True(t, cmd.isCanceled())
		assert.True(t, cmd.isCompleted())

		result := cmd.Result()
		assert.NotNil(t, result)
		assert.Nil(t, result.Value)
		assert.Equal(t, ErrCommandCanceled, result.Error)
	})

	t.Run("calls cancellation handler", func(t *testing.T) {
		vm.State().SetContext(ctx)

		var cancelCalled bool
		var cancelCmd runtime.Command

		onCancel := func(cmd runtime.Command) {
			cancelCalled = true
			cancelCmd = cmd
		}

		cmd := NewCommand(vm.State(), "test", onCancel)
		require.NotNil(t, cmd)

		err := cmd.Cancel()
		assert.NoError(t, err)
		assert.True(t, cancelCalled)
		assert.Equal(t, cmd, cancelCmd)
	})

	t.Run("returns error when already completed", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		result := &runtime.Result{Value: payload.NewPayload("test", payload.Lua)}
		err := cmd.Complete(result)
		assert.NoError(t, err)

		err = cmd.Cancel()
		assert.Error(t, err)
		assert.Equal(t, ErrCommandCompleted, err)
	})

	t.Run("returns nil when already canceled", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		err := cmd.Cancel()
		assert.NoError(t, err)

		err = cmd.Cancel()
		assert.NoError(t, err)
	})
}

func TestCommand_Result(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := newTestContext()
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	t.Run("returns nil when not completed", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		result := cmd.Result()
		assert.Nil(t, result)
	})

	t.Run("returns result when completed", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		expectedResult := &runtime.Result{
			Value: payload.NewPayload("test", payload.Lua),
			Error: nil,
		}

		err := cmd.Complete(expectedResult)
		assert.NoError(t, err)

		result := cmd.Result()
		assert.Equal(t, expectedResult, result)
	})

	t.Run("returns result when canceled", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		err := cmd.Cancel()
		assert.NoError(t, err)

		result := cmd.Result()
		assert.NotNil(t, result)
		assert.Nil(t, result.Value)
		assert.Equal(t, ErrCommandCanceled, result.Error)
	})
}

func TestCommand_StateMethods(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := newTestContext()
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	t.Run("isCompleted returns correct state", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		assert.False(t, cmd.isCompleted())

		err := cmd.Complete(&runtime.Result{Value: payload.NewPayload("test", payload.Lua)})
		assert.NoError(t, err)
		assert.True(t, cmd.isCompleted())

		cmd2 := NewCommand(vm.State(), "test2", nil)
		require.NotNil(t, cmd2)
		err = cmd2.Cancel()
		assert.NoError(t, err)
		assert.True(t, cmd2.isCompleted())
	})

	t.Run("isCanceled returns correct state", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		assert.False(t, cmd.isCanceled())

		err := cmd.Cancel()
		assert.NoError(t, err)
		assert.True(t, cmd.isCanceled())

		cmd2 := NewCommand(vm.State(), "test2", nil)
		require.NotNil(t, cmd2)
		err = cmd2.Complete(&runtime.Result{Value: payload.NewPayload("test", payload.Lua)})
		assert.NoError(t, err)
		assert.False(t, cmd2.isCanceled())
	})
}

func TestCommand_IDAndType(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := newTestContext()
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	t.Run("ID and Type return correct values", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test_type", nil)
		require.NotNil(t, cmd)

		assert.Contains(t, cmd.ID(), "cmd.test_type.")
		assert.Equal(t, runtime.Type("test_type"), cmd.Type())
	})

	t.Run("ID is unique for different commands", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd1 := NewCommand(vm.State(), "test", nil)
		cmd2 := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd1)
		require.NotNil(t, cmd2)

		assert.NotEqual(t, cmd1.ID(), cmd2.ID())
	})
}

func TestCommand_Params(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := newTestContext()
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	t.Run("Params returns correct parameters", func(t *testing.T) {
		vm.State().SetContext(ctx)

		params := []payload.Payload{
			payload.NewPayload("string", payload.Lua),
			payload.NewPayload(123, payload.Lua),
			payload.NewPayload(true, payload.Lua),
		}

		cmd := NewCommand(vm.State(), "test", nil, params...)
		require.NotNil(t, cmd)

		returnedParams := cmd.Params()
		assert.Len(t, returnedParams, 3)
		assert.Equal(t, params, returnedParams)
	})

	t.Run("Params returns empty slice when no parameters", func(t *testing.T) {
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		params := cmd.Params()
		assert.Len(t, params, 0)
	})
}
