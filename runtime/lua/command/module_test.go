package command

import (
	ctxapi "github.com/wippyai/runtime/api/context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module name", func(t *testing.T) {
		mod := NewCommandModule()
		assert.Equal(t, "command.Command", mod.Name())
	})

	t.Run("module loader", func(t *testing.T) {
		mod := NewCommandModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		_, ctx = engine.NewUnitOfWork(ctx, vm.State())

		// Test that the module can be loaded
		err = vm.DoString(ctx, `
			local command = require("command.Command")
			assert(command ~= nil)
			assert(type(command.new) == "function")
		`, "test_module_loader")
		require.NoError(t, err)
	})
}

func TestNewCommandFunc(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	// Register the command module
	RegisterCommand(vm.State())

	t.Run("creates command with string type", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		// Call newCommandFunc directly
		vm.State().Push(vm.State().NewFunction(newCommandFunc))
		vm.State().Push(lua.LString("test"))
		vm.State().Push(lua.LString("param1"))
		vm.State().Push(lua.LNumber(123))

		err := vm.State().PCall(3, 1, nil)
		require.NoError(t, err)

		// Check result
		result := vm.State().Get(-1)
		assert.Equal(t, lua.LTUserData, result.Type())

		ud := result.(*lua.LUserData)
		cmd, ok := ud.Value.(*Command)
		assert.True(t, ok)
		assert.NotNil(t, cmd)
		assert.Equal(t, "test", cmd.Type())
		assert.Len(t, cmd.Params(), 2)
	})

	t.Run("creates command with payload wrapper parameters", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		// Create a payload wrapper
		payload := payload.NewPayload("test_value", payload.Lua)
		pw := &payloadmod.Wrapper{Payload: payload}
		ud := vm.State().NewUserData()
		ud.Value = pw

		// Call newCommandFunc directly
		vm.State().Push(vm.State().NewFunction(newCommandFunc))
		vm.State().Push(lua.LString("test"))
		vm.State().Push(ud)

		err := vm.State().PCall(2, 1, nil)
		require.NoError(t, err)

		result := vm.State().Get(-1)
		assert.Equal(t, lua.LTUserData, result.Type())

		cmdUD := result.(*lua.LUserData)
		cmd, ok := cmdUD.Value.(*Command)
		assert.True(t, ok)
		assert.NotNil(t, cmd)
		assert.Len(t, cmd.Params(), 1)
		assert.Equal(t, payload, cmd.Params()[0])
	})

	t.Run("raises error for empty command type", func(t *testing.T) {
		// Call newCommandFunc directly
		vm.State().Push(vm.State().NewFunction(newCommandFunc))
		vm.State().Push(lua.LString(""))

		err := vm.State().PCall(1, 1, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "command type cannot be empty")
	})
}

func TestResponseFunc(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	RegisterCommand(vm.State())

	t.Run("returns response channel", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		// Create a command
		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		// Wrap command for Lua
		cmdUD := WrapCommand(vm.State(), cmd)

		// Call responseFunc directly
		vm.State().Push(vm.State().NewFunction(responseFunc))
		vm.State().Push(cmdUD)

		err := vm.State().PCall(1, 1, nil)
		require.NoError(t, err)

		// Check result
		result := vm.State().Get(-1)
		assert.Equal(t, lua.LTUserData, result.Type())
		assert.Equal(t, cmd.channelValue, result)
	})

	t.Run("raises error for invalid userdata", func(t *testing.T) {
		// Create invalid userdata
		invalidUD := vm.State().NewUserData()
		invalidUD.Value = "not a command"

		// Call responseFunc directly
		vm.State().Push(vm.State().NewFunction(responseFunc))
		vm.State().Push(invalidUD)

		err := vm.State().PCall(1, 1, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "command expected")
	})
}

func TestIsCompleteFunc(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	RegisterCommand(vm.State())

	t.Run("returns false for incomplete command", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		cmdUD := WrapCommand(vm.State(), cmd)

		// Call isCompleteFunc directly
		vm.State().Push(vm.State().NewFunction(isCompleteFunc))
		vm.State().Push(cmdUD)

		err := vm.State().PCall(1, 1, nil)
		require.NoError(t, err)

		result := vm.State().Get(-1)
		assert.Equal(t, lua.LTBool, result.Type())
		assert.Equal(t, lua.LBool(false), result)
	})

	t.Run("returns true for completed command", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		err := cmd.Complete(&runtime.Result{Value: payload.NewPayload("test", payload.Lua)})
		require.NoError(t, err)

		cmdUD := WrapCommand(vm.State(), cmd)

		// Call isCompleteFunc directly
		vm.State().Push(vm.State().NewFunction(isCompleteFunc))
		vm.State().Push(cmdUD)

		err = vm.State().PCall(1, 1, nil)
		require.NoError(t, err)

		result := vm.State().Get(-1)
		assert.Equal(t, lua.LTBool, result.Type())
		assert.Equal(t, lua.LBool(true), result)
	})

	t.Run("returns true for canceled command", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		err := cmd.Cancel()
		require.NoError(t, err)

		cmdUD := WrapCommand(vm.State(), cmd)

		// Call isCompleteFunc directly
		vm.State().Push(vm.State().NewFunction(isCompleteFunc))
		vm.State().Push(cmdUD)

		err = vm.State().PCall(1, 1, nil)
		require.NoError(t, err)

		result := vm.State().Get(-1)
		assert.Equal(t, lua.LTBool, result.Type())
		assert.Equal(t, lua.LBool(true), result)
	})
}

func TestResultFunc(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	RegisterCommand(vm.State())

	t.Run("returns nil and error for incomplete command", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		cmdUD := WrapCommand(vm.State(), cmd)

		// Call resultFunc directly
		vm.State().Push(vm.State().NewFunction(resultFunc))
		vm.State().Push(cmdUD)

		err := vm.State().PCall(1, 2, nil)
		require.NoError(t, err)

		payload := vm.State().Get(-2)
		errorMsg := vm.State().Get(-1)

		assert.Equal(t, lua.LTNil, payload.Type())
		assert.Equal(t, lua.LTString, errorMsg.Type())
		assert.Equal(t, "command not completed", errorMsg.String())
	})

	t.Run("returns payload and nil for successful completion", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		result := &runtime.Result{
			Value: payload.NewPayload("success", payload.Lua),
			Error: nil,
		}
		err := cmd.Complete(result)
		require.NoError(t, err)

		cmdUD := WrapCommand(vm.State(), cmd)

		// Call resultFunc directly
		vm.State().Push(vm.State().NewFunction(resultFunc))
		vm.State().Push(cmdUD)

		err = vm.State().PCall(1, 2, nil)
		require.NoError(t, err)

		payload := vm.State().Get(-2)
		errorMsg := vm.State().Get(-1)

		assert.Equal(t, lua.LTUserData, payload.Type())
		assert.Equal(t, lua.LTNil, errorMsg.Type())
	})

	t.Run("returns nil and error for failed completion", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		result := &runtime.Result{
			Value: nil,
			Error: assert.AnError,
		}
		err := cmd.Complete(result)
		require.NoError(t, err)

		cmdUD := WrapCommand(vm.State(), cmd)

		// Call resultFunc directly
		vm.State().Push(vm.State().NewFunction(resultFunc))
		vm.State().Push(cmdUD)

		err = vm.State().PCall(1, 2, nil)
		require.NoError(t, err)

		payload := vm.State().Get(-2)
		errorMsg := vm.State().Get(-1)

		assert.Equal(t, lua.LTNil, payload.Type())
		assert.Equal(t, lua.LTString, errorMsg.Type())
		assert.Equal(t, assert.AnError.Error(), errorMsg.String())
	})

	t.Run("returns empty payload for nil result value", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		result := &runtime.Result{
			Value: nil,
			Error: nil,
		}
		err := cmd.Complete(result)
		require.NoError(t, err)

		cmdUD := WrapCommand(vm.State(), cmd)

		// Call resultFunc directly
		vm.State().Push(vm.State().NewFunction(resultFunc))
		vm.State().Push(cmdUD)

		err = vm.State().PCall(1, 2, nil)
		require.NoError(t, err)

		payload := vm.State().Get(-2)
		errorMsg := vm.State().Get(-1)

		assert.Equal(t, lua.LTUserData, payload.Type())
		assert.Equal(t, lua.LTNil, errorMsg.Type())
	})
}

func TestIsCanceledFunc(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	RegisterCommand(vm.State())

	t.Run("returns false for non-canceled command", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		cmdUD := WrapCommand(vm.State(), cmd)

		// Call isCanceledFunc directly
		vm.State().Push(vm.State().NewFunction(isCanceledFunc))
		vm.State().Push(cmdUD)

		err := vm.State().PCall(1, 1, nil)
		require.NoError(t, err)

		result := vm.State().Get(-1)
		assert.Equal(t, lua.LTBool, result.Type())
		assert.Equal(t, lua.LBool(false), result)
	})

	t.Run("returns true for canceled command", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		err := cmd.Cancel()
		require.NoError(t, err)

		cmdUD := WrapCommand(vm.State(), cmd)

		// Call isCanceledFunc directly
		vm.State().Push(vm.State().NewFunction(isCanceledFunc))
		vm.State().Push(cmdUD)

		err = vm.State().PCall(1, 1, nil)
		require.NoError(t, err)

		result := vm.State().Get(-1)
		assert.Equal(t, lua.LTBool, result.Type())
		assert.Equal(t, lua.LBool(true), result)
	})
}

func TestCancelFunc(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	RegisterCommand(vm.State())

	t.Run("cancels command successfully", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		cmdUD := WrapCommand(vm.State(), cmd)

		// Call cancelFunc directly
		vm.State().Push(vm.State().NewFunction(cancelFunc))
		vm.State().Push(cmdUD)

		err := vm.State().PCall(1, 2, nil)
		require.NoError(t, err)

		success := vm.State().Get(-2)
		errorMsg := vm.State().Get(-1)

		assert.Equal(t, lua.LTBool, success.Type())
		assert.Equal(t, lua.LBool(true), success)
		assert.Equal(t, lua.LTNil, errorMsg.Type())
		assert.True(t, cmd.isCanceled())
	})

	t.Run("returns error when command already completed", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		cmd := NewCommand(vm.State(), "test", nil)
		err := cmd.Complete(&runtime.Result{Value: payload.NewPayload("test", payload.Lua)})
		require.NoError(t, err)

		cmdUD := WrapCommand(vm.State(), cmd)

		// Call cancelFunc directly
		vm.State().Push(vm.State().NewFunction(cancelFunc))
		vm.State().Push(cmdUD)

		err = vm.State().PCall(1, 2, nil)
		require.NoError(t, err)

		success := vm.State().Get(-2)
		errorMsg := vm.State().Get(-1)

		assert.Equal(t, lua.LTBool, success.Type())
		assert.Equal(t, lua.LBool(false), success)
		assert.Equal(t, lua.LTString, errorMsg.Type())
		assert.Equal(t, ErrCommandCompleted.Error(), errorMsg.String())
	})
}

func TestCheckCommand(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	t.Run("returns command for valid userdata", func(t *testing.T) {
		cmd := &Command{id: "test"}
		ud := vm.State().NewUserData()
		ud.Value = cmd

		vm.State().Push(ud)
		result := CheckCommand(vm.State())
		assert.Equal(t, cmd, result)
	})

	t.Run("raises error for invalid userdata", func(t *testing.T) {
		ud := vm.State().NewUserData()
		ud.Value = "not a command"

		vm.State().Push(ud)

		// CheckCommand calls l.ArgError which raises a Lua error
		// We need to call it in a way that will actually trigger the error
		vm.State().Push(vm.State().NewFunction(func(l *lua.LState) int {
			CheckCommand(l)
			return 0
		}))

		err := vm.State().PCall(0, 0, nil)
		assert.Error(t, err)
		// The error message should contain the argument error from CheckCommand
		assert.Contains(t, err.Error(), "bad argument")
	})
}

func TestWrapCommand(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	// Register the command module to ensure metatable is available
	RegisterCommand(vm.State())

	t.Run("wraps command correctly", func(t *testing.T) {
		cmd := &Command{id: "test"}
		ud := WrapCommand(vm.State(), cmd)

		assert.Equal(t, lua.LTUserData, ud.Type())
		assert.Equal(t, cmd, ud.Value)
		assert.NotNil(t, ud.Metatable)
	})
}

func TestIntegration(t *testing.T) {
	logger := zap.NewNop()
	vm, err := engine.NewVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_, ctx = engine.NewUnitOfWork(ctx, vm.State())

	// Register the command module directly
	RegisterCommand(vm.State())

	t.Run("full command lifecycle", func(t *testing.T) {
		// Set the unit of work context in the Lua state
		vm.State().SetContext(ctx)

		// Create a command directly in Go first to ensure it works
		cmd := NewCommand(vm.State(), "test", nil)
		require.NotNil(t, cmd)

		// Test the command lifecycle in Go
		assert.False(t, cmd.isCompleted())
		assert.False(t, cmd.isCanceled())

		// Cancel the command
		err := cmd.Cancel()
		require.NoError(t, err)

		assert.True(t, cmd.isCompleted())
		assert.True(t, cmd.isCanceled())

		// Test Lua integration with the created command
		cmdUD := WrapCommand(vm.State(), cmd)
		require.NotNil(t, cmdUD)

		// Set the command in Lua global state
		vm.State().SetGlobal("test_cmd", cmdUD)

		// Test Lua methods work correctly
		err = vm.DoString(ctx, `
			-- Test that we can access the command methods
			local function test_command_methods(cmd)
				if cmd == nil then
					error("Command is nil")
				end
				
				-- Test is_complete
				local is_complete = cmd:is_complete()
				if type(is_complete) ~= "boolean" then
					error("is_complete should return boolean")
				end
				
				-- Test is_canceled
				local is_canceled = cmd:is_canceled()
				if type(is_canceled) ~= "boolean" then
					error("is_canceled should return boolean")
				end
				
				-- Test response
				local channel = cmd:response()
				if channel == nil then
					error("Response channel should not be nil")
				end
				
				-- Test result
				local payload, err = cmd:result()
				if payload ~= nil then
					error("Payload should be nil for canceled command")
				end
				if err ~= "command canceled" then
					error("Expected 'command canceled' error, got: " .. tostring(err))
				end
			end
			
			-- Get the command from the global variable set by Go
			local cmd = _G.test_cmd
			test_command_methods(cmd)
		`, "test_integration")
		require.NoError(t, err)
	})
}
