package command

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestCommandLayer_BasicOperations(t *testing.T) {
	logger := zap.NewNop()

	// Create channel layer first
	channelLayer := channel.NewChannelLayer()
	commandLayer := NewCommandLayer(channelLayer)

	// Create base VM with command module and layers
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("command", NewCommandModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	// Create runner with layers before starting any Lua code
	runner := engine.NewRunner(vm,
		engine.WithLayer(channelLayer),
		engine.WithLayer(commandLayer),
	)

	// Setup context with task group
	ctx := runner.WithContext(context.Background())

	// starts (but does not run)
	err = vm.StartString(ctx, `
        -- Create and test a command
        local cmd = command.new("test")
        assert(cmd ~= nil, "command creation failed")
        
        -- Test response channel access
        local resp = cmd:response()
        assert(resp ~= nil, "failed to get response channel")
        
        coroutine.yield("command_created")
		return resp:receive()
    `, "test")
	assert.NoError(t, err)

	// execute and collect yields
	var yields []string
	tasks, err := runner.Step()
	assert.NoError(t, err)

	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}

		// Check pending commands after creation
		if contains(yields, "command_created") {
			pending := commandLayer.GetPendingCommands()
			assert.Equal(t, 1, len(pending), "should have one pending command")
			assert.Equal(t, Type("test"), pending[0].cmdType)
		}

		tasks, err = runner.Step(tasks...)
		assert.NoError(t, err)
	}

	assert.Contains(t, yields, "command_created")
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func TestCommandLayer_Context(t *testing.T) {
	// Create channel layer and command layer
	channelLayer := channel.NewChannelLayer()
	commandLayer := NewCommandLayer(channelLayer)

	// Test WithContext
	ctx := context.Background()
	enrichedCtx := commandLayer.WithContext(ctx)
	assert.NotNil(t, enrichedCtx, "context enrichment should succeed")

	// Test GetCommandContext with valid context
	retrievedLayer := GetCommandContext(enrichedCtx)
	assert.NotNil(t, retrievedLayer, "should retrieve command layer from context")
	assert.Same(t, commandLayer, retrievedLayer, "should retrieve the same command layer instance")

	// Test GetCommandContext with nil context
	nilCtxLayer := GetCommandContext(nil)
	assert.Nil(t, nilCtxLayer, "should return nil for nil context")

	// Test GetCommandContext with context missing command layer
	emptyCtxLayer := GetCommandContext(context.Background())
	assert.Nil(t, emptyCtxLayer, "should return nil for context without command layer")

	// Test GetCommandContext with context containing wrong value type
	wrongCtx := context.WithValue(ctx, cmdCtxKey, "not a layer")
	wrongTypeLayer := GetCommandContext(wrongCtx)
	assert.Nil(t, wrongTypeLayer, "should return nil for context with wrong value type")
}

func TestLayer_MultipleConcurrentCommands(t *testing.T) {
	logger := zap.NewNop()

	// Create channel and command layers
	channelLayer := channel.NewChannelLayer()
	commandLayer := NewCommandLayer(channelLayer)

	// Create base VM with required modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("command", NewCommandModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	// Create runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(channelLayer),
		engine.WithLayer(commandLayer),
	)

	// Setup context with task group
	ctx := runner.WithContext(context.Background())

	// Start VM with script that creates multiple commands
	err = vm.StartString(ctx, `
		-- Create multiple commands
		local cmd1 = command.new("test1", {value = "first"})
		local cmd2 = command.new("test2", {value = "second"})
		local cmd3 = command.new("test3", {value = "third"})
		
		assert(cmd1 ~= nil, "command1 creation failed")
		assert(cmd2 ~= nil, "command2 creation failed")
		assert(cmd3 ~= nil, "command3 creation failed")
		
		-- Store response channels
		local resp1 = cmd1:response()
		local resp2 = cmd2:response()
		local resp3 = cmd3:response()
		
		-- Yield to allow processing
		coroutine.yield("commands_created")
		
		-- Receive results from all channels
		local result1, ok1 = resp1:receive()
		local result2, ok2 = resp2:receive()
		local result3, ok3 = resp3:receive()
		
		-- Return all results
		return result1, result2, result3
	`, "test")
	assert.NoError(t, err)

	// execute and collect yields
	var yields []string
	tasks, err := runner.Step()
	assert.NoError(t, err)

	// Track command processing
	var pendingCommands []*Command

	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}

		// After commands are created, verify pending commands and process them
		if contains(yields, "commands_created") {
			// Get and verify pending commands
			pendingCommands = commandLayer.GetPendingCommands()
			assert.Equal(t, 3, len(pendingCommands), "should have three pending commands")

			// Verify command types
			var cmdTypes []Type
			for _, cmd := range pendingCommands {
				cmdTypes = append(cmdTypes, cmd.cmdType)
				assert.False(t, cmd.IsComplete(), "command should not be complete initially")
			}
			assert.Contains(t, cmdTypes, Type("test1"))
			assert.Contains(t, cmdTypes, Type("test2"))
			assert.Contains(t, cmdTypes, Type("test3"))

			// Process commands with results
			for _, cmd := range pendingCommands {
				result := lua.LString(fmt.Sprintf("result_%s", cmd.cmdType))
				commandLayer.QueueResult(cmd, result)
				assert.True(t, cmd.IsComplete(), "command should be complete after processing")
			}
		}

		tasks, err = runner.Step(tasks...)
		assert.NoError(t, err)
	}

	// Verify the sequence of events
	assert.Contains(t, yields, "commands_created", "should yield after creating commands")

	// Verify that all commands were processed
	assert.NotNil(t, pendingCommands, "should have processed commands")
	if len(pendingCommands) > 0 {
		for _, cmd := range pendingCommands {
			result, err := cmd.Result()
			assert.NoError(t, err, "should not have error getting result")
			assert.NotNil(t, result, "should have result")
			assert.Equal(t, fmt.Sprintf("result_%s", cmd.cmdType), result.String(),
				"should have correct result for command type")
		}
	}
}

func TestCommandLayer_ErrorPropagation(t *testing.T) {
	logger := zap.NewNop()

	// Create layers
	channelLayer := channel.NewChannelLayer()
	commandLayer := NewCommandLayer(channelLayer)

	// Create VM with modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("command", NewCommandModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	// Create runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(channelLayer),
		engine.WithLayer(commandLayer),
	)

	// Setup context
	ctx := runner.WithContext(context.Background())

	// Start VM with script that creates a command and waits for result
	err = vm.StartString(ctx, `
		local cmd = command.new("test_error")
		assert(cmd ~= nil, "command creation failed")
		
		local resp = cmd:response()
		coroutine.yield("command_created")
		
		-- Check command state
		assert(cmd:is_complete(), "command should be complete")
		local err = cmd:error()
		assert(err ~= nil, "should have error message")
		assert(string.find(err, "test error"), "error message should match")
		
		-- Try to receive result, should get error
		local result, ok = resp:receive()
		assert(not ok, "should receive error status")
		return ok  -- should be false indicating error
	`, "test")
	assert.NoError(t, err)

	// Run and verify error propagation
	tasks, err := runner.Step()
	assert.NoError(t, err)

	var cmd *Command
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 && task.Yielded[0].String() == "command_created" {
				// Get the command and queue an error
				pending := commandLayer.GetPendingCommands()
				assert.Equal(t, 1, len(pending), "should have one pending command")

				cmd = pending[0]
				testErr := fmt.Errorf("test error for command")
				commandLayer.QueueError(cmd, testErr)

				// Verify error was set correctly
				assert.True(t, cmd.IsComplete())
				assert.Equal(t, testErr, cmd.Err())
			}
		}

		tasks, err = runner.Step(tasks...)
		assert.NoError(t, err)
	}

	// Final verification of command state
	assert.NotNil(t, cmd)
	result, err := cmd.Result()
	assert.Error(t, err, "should have error")
	assert.Nil(t, result, "result should be nil when error is set")
}

func TestCommand_LuaMethodsComplete(t *testing.T) {
	logger := zap.NewNop()

	// Create layers
	channelLayer := channel.NewChannelLayer()
	commandLayer := NewCommandLayer(channelLayer)

	// Create VM with modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("command", NewCommandModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	// Create runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(channelLayer),
		engine.WithLayer(commandLayer),
	)

	// Setup context
	ctx := runner.WithContext(context.Background())

	// Start VM with script that tests all command methods
	err = vm.StartString(ctx, `
		-- Create test command
		local cmd = command.new("test_methods")
		assert(cmd ~= nil, "command creation failed")
		
		-- Initial state checks
		assert(not cmd:is_complete(), "command should not be complete initially")
		assert(not cmd:is_canceled(), "command should not be canceled initially")
		assert(cmd:error() == nil, "should have no error initially")
		
		local result, err = cmd:result()
		assert(result == nil and err ~= nil, "incomplete command should error on result")
		
		coroutine.yield("initial_checks_done")
		
		-- After completion checks
		assert(cmd:is_complete(), "command should be complete")
		assert(not cmd:is_canceled(), "command should not be canceled")
		assert(cmd:error() == nil, "successful command should have no error")
		
		result, err = cmd:result()
		assert(result == "success" and err == nil, "should have success result")
		
		coroutine.yield("success_checks_done")
		
		-- Create another command for error case
		local cmd_err = command.new("test_error")
		coroutine.yield("error_command_created")
		
		-- Error case checks
		assert(cmd_err:is_complete(), "error command should be complete")
		assert(cmd_err:error() ~= nil, "should have error message")
		
		result, err = cmd_err:result()
		assert(result == nil and err ~= nil, "should have error instead of result")
		
		coroutine.yield("error_checks_done")
		
		-- Create command to test cancellation
		local cmd_cancel = command.new("test_cancel")
		coroutine.yield("cancel_command_created")
		
		-- Cancellation checks
		assert(cmd_cancel:is_complete(), "canceled command should be complete")
		assert(cmd_cancel:is_canceled(), "should be marked as canceled")
		assert(cmd_cancel:error() ~= nil, "canceled command should have error")
		
		result, err = cmd_cancel:result()
		assert(result == nil and err ~= nil, "canceled command should error on result")
		
		return "all_tests_complete"
	`, "test")
	assert.NoError(t, err)

	// Run and verify all cases
	tasks, err := runner.Step()
	assert.NoError(t, err)

	var currentCmd *Command
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yield := task.Yielded[0].String()

				switch yield {
				case "initial_checks_done":
					// Get first command and set success result
					pending := commandLayer.GetPendingCommands()
					assert.Equal(t, 1, len(pending))
					currentCmd = pending[0]
					commandLayer.QueueResult(currentCmd, lua.LString("success"))

				case "error_command_created":
					// Get error command and queue error
					pending := commandLayer.GetPendingCommands()
					assert.Equal(t, 1, len(pending))
					currentCmd = pending[0]
					commandLayer.QueueError(currentCmd, fmt.Errorf("test error"))

				case "cancel_command_created":
					// Get cancel command and mark as canceled
					pending := commandLayer.GetPendingCommands()
					assert.Equal(t, 1, len(pending))
					currentCmd = pending[0]
					currentCmd.Cancel()

				}
			}
		}

		tasks, err = runner.Step(tasks...)
		assert.NoError(t, err)
	}
}

func TestCommandLayer_SelectOperations(t *testing.T) {
	var executionFlow []string
	addToFlow := func(step string) {
		executionFlow = append(executionFlow, step)
	}

	channelLayer := channel.NewChannelLayer()
	commandLayer := NewCommandLayer(channelLayer)

	vm, err := engine.NewCVM(
		zap.NewNop(),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("command", NewCommandModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	runner := engine.NewRunner(vm,
		engine.WithLayer(channelLayer),
		engine.WithLayer(commandLayer),
	)

	ctx := runner.WithContext(context.Background())
	addToFlow("test_setup_complete")

	err = vm.StartString(ctx, `
		local cmd1 = command.new("select1", {value = "first"})
		local cmd2 = command.new("select2", {value = "second"})
		
		local resp1 = cmd1:response()
		local resp2 = cmd2:response()
		local control_ch = channel.new()
		
		assert(not cmd1:is_complete(), "cmd1 should not be complete initially")
		assert(not cmd2:is_complete(), "cmd2 should not be complete initially")
		coroutine.yield("initial_state_verified")
		
		local result = channel.select{
			resp1:case_receive(),
			resp2:case_receive(),
			control_ch:case_receive()
		}
		assert(result.channel == resp1, "First select should choose resp1")
		assert(result.value == "result1", "Expected result1")
		coroutine.yield("first_select_complete")
		
		result = channel.select{
			resp2:case_receive(),
			control_ch:case_receive()
		}
		assert(result.channel == resp2, "Second select should choose resp2")
		assert(result.value == "result2", "Expected result2")
		coroutine.yield("second_select_complete")
		
		result = channel.select{
			control_ch:case_receive(),
			default = true
		}
		assert(result.default, "Should hit default case when no messages available")
		coroutine.yield("default_select_complete")
		
		assert(cmd1:is_complete(), "cmd1 should be complete")
		assert(cmd2:is_complete(), "cmd2 should be complete")
		
		local result1, err1 = cmd1:result()
		assert(result1 == "result1", "cmd1 should have correct result")
		assert(err1 == nil, "cmd1 should have no error")
		
		local result2, err2 = cmd2:result()
		assert(result2 == "result2", "cmd2 should have correct result")
		assert(err2 == nil, "cmd2 should have no error")
		
		coroutine.yield("final_verification_complete")
	`, "test")
	assert.NoError(t, err)
	addToFlow("lua_script_loaded")

	tasks, err := runner.Step()
	assert.NoError(t, err)
	addToFlow("initial_step_complete")

	var commands []*Command
	stepCount := 0

	stateVerified := false
	firstSelectDone := false
	secondSelectDone := false
	defaultSelectDone := false
	finalVerificationDone := false

	for len(tasks) > 0 {
		stepCount++
		addToFlow(fmt.Sprintf("starting_step_%d", stepCount))

		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yield := task.Yielded[0].String()
				addToFlow(fmt.Sprintf("yield_%s", yield))

				switch yield {
				case "initial_state_verified":
					stateVerified = true
					commands = commandLayer.GetPendingCommands()
					assert.Equal(t, 2, len(commands))
					assert.Equal(t, Type("select1"), commands[0].cmdType)
					assert.Equal(t, Type("select2"), commands[1].cmdType)

					commandLayer.QueueResult(commands[0], lua.LString("result1"))
					addToFlow("first_result_queued")

				case "first_select_complete":
					firstSelectDone = true
					assert.True(t, commands[0].IsComplete())
					result, err := commands[0].Result()
					assert.NoError(t, err)
					assert.Equal(t, "result1", result.String())

					commandLayer.QueueResult(commands[1], lua.LString("result2"))
					addToFlow("second_result_queued")

				case "second_select_complete":
					secondSelectDone = true
					assert.True(t, commands[1].IsComplete())
					result, err := commands[1].Result()
					assert.NoError(t, err)
					assert.Equal(t, "result2", result.String())

				case "default_select_complete":
					defaultSelectDone = true
					for _, cmd := range commands {
						assert.True(t, cmd.IsComplete())
					}

				case "final_verification_complete":
					finalVerificationDone = true
					addToFlow("final_verification_done")

					for i, cmd := range commands {
						assert.True(t, cmd.IsComplete())
						result, err := cmd.Result()
						assert.NoError(t, err)
						assert.Equal(t, fmt.Sprintf("result%d", i+1), result.String())
					}
				}
			}
		}

		tasks, err = runner.Step(tasks...)
		assert.NoError(t, err)
		addToFlow(fmt.Sprintf("step_%d_complete", stepCount))
	}

	assert.True(t, stateVerified)
	assert.True(t, firstSelectDone)
	assert.True(t, secondSelectDone)
	assert.True(t, defaultSelectDone)
	assert.True(t, finalVerificationDone)

	requiredSteps := []string{
		"test_setup_complete",
		"lua_script_loaded",
		"initial_step_complete",
		"first_result_queued",
		"second_result_queued",
		"final_verification_done",
	}

	for _, step := range requiredSteps {
		found := false
		for _, executedStep := range executionFlow {
			if executedStep == step {
				found = true
				break
			}
		}
		assert.True(t, found)
	}
}
