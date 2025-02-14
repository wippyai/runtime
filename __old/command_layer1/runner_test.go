package command_layer1

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/command"
	"github.com/ponyruntime/pony/runtime/lua/engine/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestWorkflowRunner_BasicFlow(t *testing.T) {
	logger := zap.NewNop()

	// Spawn VM with required modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("command", command.NewCommandModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Spawn layers
	channels := channel.NewChannelLayer()
	cmdLayer := command.NewCommandLayer(channels)
	pubLayer := pubsub.NewSubscriptionLayer(channels)

	// Spawn runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(cmdLayer),
		engine.WithLayer(pubLayer),
	)

	// Spawn workflow runner
	workflow := NewWorkflowRunner(runner, cmdLayer, pubLayer)

	// Define test script
	script := `
        function test_workflow()
            -- Spawn a command to process
            local cmd = command.new("test_command", {value = "hello"})
            local resp = cmd:response()
            
            -- Wait for response
            local result = resp:receive()
            return result .. " world"
        end
    `

	// Import script
	err = vm.Import(script, "test", "test_workflow")
	require.NoError(t, err)

	// Launch workflow
	err = workflow.Start(context.Background(), "test_workflow")
	require.NoError(t, err)

	var processedCmd *command.Command

	// Run workflow until completion
	for !workflow.IsComplete() {
		cmds, err := workflow.Step()

		require.NoError(t, err)

		// Process commands if any
		for _, cmd := range cmds {
			// Verify command type and value
			assert.Equal(t, command.Type("test_command"), cmd.CmdType())
			processedCmd = cmd
		}

		// Set command result if we have a processed command
		if processedCmd != nil {
			err = workflow.SendResult(processedCmd, lua.LString("hello"))
			require.NoError(t, err)
			processedCmd = nil
		}
	}

	// Verify completion and result
	assert.True(t, workflow.IsComplete())
	result, err := workflow.GetCompletionResult()
	require.NoError(t, err)
	assert.Equal(t, "hello world", result.String())
}

func TestWorkflowRunner_SequentialCommands(t *testing.T) {
	logger := zap.NewNop()

	// Spawn VM with required modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("command", command.NewCommandModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Spawn layers
	channels := channel.NewChannelLayer()
	cmdLayer := command.NewCommandLayer(channels)
	pubLayer := pubsub.NewSubscriptionLayer(channels)

	// Spawn runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(cmdLayer),
		engine.WithLayer(pubLayer),
	)

	// Spawn workflow runner
	workflow := NewWorkflowRunner(runner, cmdLayer, pubLayer)

	// Define test script with sequential commands
	script := `
        function test_workflow()
            -- First command
            local cmd1 = command.new("first_command", {value = "step1"})
            local resp1 = cmd1:response()
            local result1 = resp1:receive()
            
            -- Second command using result from first
            local cmd2 = command.new("second_command", {value = result1})
            local resp2 = cmd2:response()
            local result2 = resp2:receive()
            
            return result1 .. " -> " .. result2
        end
    `

	// Import script
	err = vm.Import(script, "test", "test_workflow")
	require.NoError(t, err)

	// Launch workflow
	err = workflow.Start(context.Background(), "test_workflow")
	require.NoError(t, err)

	var commandCount int

	// Run workflow until completion
	for !workflow.IsComplete() {
		cmds, err := workflow.Step()
		require.NoError(t, err)

		// Process commands if any
		for _, cmd := range cmds {
			commandCount++

			switch cmd.CmdType() {
			case "first_command":
				assert.Equal(t, 1, commandCount, "first command should be processed first")
				err = workflow.SendResult(cmd, lua.LString("hello"))
				require.NoError(t, err)

			case "second_command":
				assert.Equal(t, 2, commandCount, "second command should be processed second")
				err = workflow.SendResult(cmd, lua.LString("world"))
				require.NoError(t, err)

			default:
				t.Fatalf("unexpected command type: %s", cmd.CmdType())
			}
		}
	}

	// Verify completion and result
	assert.True(t, workflow.IsComplete())
	assert.Equal(t, 2, commandCount, "should process exactly two commands")

	result, err := workflow.GetCompletionResult()
	require.NoError(t, err)
	assert.Equal(t, "hello -> world", result.String())
}

func TestWorkflowRunner_CommandFailure(t *testing.T) {
	logger := zap.NewNop()

	// Spawn VM with required modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("command", command.NewCommandModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Spawn layers
	channels := channel.NewChannelLayer()
	cmdLayer := command.NewCommandLayer(channels)
	pubLayer := pubsub.NewSubscriptionLayer(channels)

	// Spawn runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(cmdLayer),
		engine.WithLayer(pubLayer),
	)

	// Spawn workflow runner
	workflow := NewWorkflowRunner(runner, cmdLayer, pubLayer)

	// Define test script that handles command errors
	script := `
        function test_workflow()
            -- First command that will fail
            local cmd1 = command.new("failing_command")
            local resp1 = cmd1:response()
            
            -- Check for error
            local result, ok = resp1:receive()
            if not ok then
                -- Command failed, get error message
                local err = cmd1:error()
                return "Command failed: " .. err
            end
            
            return "Should not reach here"
        end
    `

	// Import script
	err = vm.Import(script, "test", "test_workflow")
	require.NoError(t, err)

	// Launch workflow
	err = workflow.Start(context.Background(), "test_workflow")
	require.NoError(t, err)

	// Run workflow until completion
	for !workflow.IsComplete() {
		cmds, err := workflow.Step()
		require.NoError(t, err)

		// Process commands if any
		for _, cmd := range cmds {
			assert.Equal(t, command.Type("failing_command"), cmd.CmdType())

			// Simulate command failure
			err = workflow.SendError(cmd, fmt.Errorf("simulated error"))
			require.NoError(t, err)
		}
	}

	// Verify completion and result
	assert.True(t, workflow.IsComplete())
	result, err := workflow.GetCompletionResult()
	require.NoError(t, err)
	assert.Equal(t, "Command failed: simulated error", result.String())
}

func TestWorkflowRunner_ConcurrentCommandsWithSelect(t *testing.T) {
	logger := zap.NewNop()

	// Spawn VM with required modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("command", command.NewCommandModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Spawn layers
	channels := channel.NewChannelLayer()
	cmdLayer := command.NewCommandLayer(channels)
	pubLayer := pubsub.NewSubscriptionLayer(channels)

	// Spawn runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(cmdLayer),
		engine.WithLayer(pubLayer),
	)

	// Spawn workflow runner
	workflow := NewWorkflowRunner(runner, cmdLayer, pubLayer)

	// Define test script with concurrent commands and select
	script := `
        function test_workflow()
            -- Schedule both commands upfront
            local cmd1 = command.new("first_command")
            local cmd2 = command.new("second_command")
            
            local resp1 = cmd1:response()
            local resp2 = cmd2:response()
            
            local results = {}
            local pending = {
                resp1 = true,
                resp2 = true
            }
            
            -- Loop until we get both results
            while #results < 2 do
                local cases = {}
                
                -- Only add channels that haven't completed
                if pending.resp1 then
                    table.insert(cases, resp1:case_receive())
                end
                if pending.resp2 then
                    table.insert(cases, resp2:case_receive())
                end
                
                local result = channel.select(cases)
                
                -- Identify which response we got and remove it from pending
                if result.channel == resp1 then
                    pending.resp1 = false
                    table.insert(results, result.value)
                elseif result.channel == resp2 then
                    pending.resp2 = false
                    table.insert(results, result.value)
                end
            end
            
            return results
        end
    `

	// Import script
	err = vm.Import(script, "test", "test_workflow")
	require.NoError(t, err)

	// Launch workflow
	err = workflow.Start(context.Background(), "test_workflow")
	require.NoError(t, err)

	var (
		firstCmd  *command.Command
		secondCmd *command.Command
	)

	// First step should give us both commands
	cmds, err := workflow.Step()
	require.NoError(t, err)
	require.Len(t, cmds, 2)

	// Store commands by type
	for _, cmd := range cmds {
		switch cmd.CmdType() {
		case "first_command":
			firstCmd = cmd
		case "second_command":
			secondCmd = cmd
		}
	}

	require.NotNil(t, firstCmd, "should have first command")
	require.NotNil(t, secondCmd, "should have second command")

	// Send response to second command first
	err = workflow.SendResult(secondCmd, lua.LString("world"))
	require.NoError(t, err)

	// Step to process first response
	cmds, err = workflow.Step()
	require.NoError(t, err)
	require.Empty(t, cmds)

	// Send response to first command
	err = workflow.SendResult(firstCmd, lua.LString("hello"))
	require.NoError(t, err)

	// Run until completion
	for !workflow.IsComplete() {
		cmds, err = workflow.Step()
		require.NoError(t, err)
		require.Empty(t, cmds)
	}

	// Verify completion and results
	assert.True(t, workflow.IsComplete())
	result, err := workflow.GetCompletionResult()
	require.NoError(t, err)

	// Results should be in order of responses, not command creation
	resultTable := result.(*lua.LTable)
	assert.Equal(t, "world", resultTable.RawGetInt(1).String(), "first result should be from second command")
	assert.Equal(t, "hello", resultTable.RawGetInt(2).String(), "second result should be from first command")
}

func TestWorkflowRunner_CommandWithSignal(t *testing.T) {
	logger := zap.NewNop()

	// Spawn VM with required modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("command", command.NewCommandModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("pubsub", pubsub.NewModule().Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Spawn layers
	channels := channel.NewChannelLayer()
	cmdLayer := command.NewCommandLayer(channels)
	pubLayer := pubsub.NewSubscriptionLayer(channels)

	// Spawn runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(cmdLayer),
		engine.WithLayer(pubLayer),
	)

	// Spawn workflow runner
	workflow := NewWorkflowRunner(runner, cmdLayer, pubLayer)

	// Define test script that waits for both command and signal
	script := `
        function test_workflow()
            -- Schedule command first
            local cmd = command.new("test_command")
            local resp = cmd:response()
            
            -- Subscribe to signal
            local signal = pubsub.subscribe("test.signal")
            
            -- Wait for signal first
            local signal_value = signal:receive()
            
            -- Then get command result
            local cmd_result = resp:receive()
            
            return cmd_result .. " - " .. signal_value
        end
    `

	// Import script
	err = vm.Import(script, "test", "test_workflow")
	require.NoError(t, err)

	// Launch workflow
	err = workflow.Start(context.Background(), "test_workflow")
	require.NoError(t, err)

	var processedCmd *command.Command

	// First step should give us the command
	cmds, err := workflow.Step()
	require.NoError(t, err)
	require.Len(t, cmds, 1)

	processedCmd = cmds[0]
	assert.Equal(t, command.Type("test_command"), processedCmd.CmdType())

	// Set command result
	err = workflow.SendResult(processedCmd, lua.LString("hello"))
	require.NoError(t, err)

	// Step to process command result
	cmds, err = workflow.Step()
	require.NoError(t, err)
	require.Empty(t, cmds)

	// Send signal
	err = workflow.SendValue("test.signal", lua.LString("world"))
	require.NoError(t, err)

	// Run until completion
	for !workflow.IsComplete() {
		cmds, err = workflow.Step()
		require.NoError(t, err)
		require.Empty(t, cmds)
	}

	// Verify completion and result
	assert.True(t, workflow.IsComplete())
	result, err := workflow.GetCompletionResult()
	require.NoError(t, err)
	assert.Equal(t, "hello - world", result.String())
}

func TestWorkflowRunner_CommandWithSignalCounter(t *testing.T) {
	logger := zap.NewNop()

	// Spawn VM with required modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("command", command.NewCommandModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("pubsub", pubsub.NewModule().Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Spawn layers
	channels := channel.NewChannelLayer()
	cmdLayer := command.NewCommandLayer(channels)
	pubLayer := pubsub.NewSubscriptionLayer(channels)

	// Spawn runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(cmdLayer),
		engine.WithLayer(pubLayer),
	)

	// Spawn workflow runner
	workflow := NewWorkflowRunner(runner, cmdLayer, pubLayer)

	// Define test script with parallel signal counting
	script := `
        function test_workflow() 
            local signal_count = 0
            
            -- Launch signal counter coroutine
            coroutine.spawn(function()
                local count = 0
                local signals = pubsub.subscribe("test.signal")
                
                -- Count signals until channel is closed
                while true do
                    local value, ok = signals:receive()
                    if not ok then break end
                    signal_count = signal_count + 1
                end
            end)
            
            -- Main flow: handle command
            local cmd = command.new("test_command")
            local resp = cmd:response()
            local cmd_result = resp:receive()
            
            return {
                command_result = cmd_result,
                signal_count = signal_count
            }
        end
    `

	// Import script
	err = vm.Import(script, "test", "test_workflow")
	require.NoError(t, err)

	// Launch workflow
	err = workflow.Start(context.Background(), "test_workflow")
	require.NoError(t, err)

	var processedCmd *command.Command

	// First step should give us the command
	cmds, err := workflow.Step()
	require.NoError(t, err)
	require.Len(t, cmds, 1)

	processedCmd = cmds[0]
	assert.Equal(t, command.Type("test_command"), processedCmd.CmdType())

	// Send a few signals
	err = workflow.SendValue("test.signal", lua.LString("signal1"))
	require.NoError(t, err)

	err = workflow.SendValue("test.signal", lua.LString("signal2"))
	require.NoError(t, err)

	// Step to process signals
	cmds, err = workflow.Step()
	require.NoError(t, err)
	require.Empty(t, cmds)

	// Set command result
	err = workflow.SendResult(processedCmd, lua.LString("hello"))
	require.NoError(t, err)

	// Send one more signal
	err = workflow.SendValue("test.signal", lua.LString("signal3"))
	require.NoError(t, err)

	// Run until completion
	for !workflow.IsComplete() {
		cmds, err = workflow.Step()
		require.NoError(t, err)
		require.Empty(t, cmds)
	}

	// Verify completion and results
	assert.True(t, workflow.IsComplete())
	result, err := workflow.GetCompletionResult()
	require.NoError(t, err)

	resultTable := result.(*lua.LTable)
	assert.Equal(t, "hello", resultTable.RawGetString("command_result").String(),
		"should have correct command result")
	assert.Equal(t, float64(3), float64(resultTable.RawGetString("signal_count").(lua.LNumber)),
		"should have counted 3 signals")
}

func TestWorkflowRunner_ExitFromFunction(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("command", command.NewCommandModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	channels := channel.NewChannelLayer()
	cmdLayer := command.NewCommandLayer(channels)
	pubLayer := pubsub.NewSubscriptionLayer(channels)

	runner := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(cmdLayer),
		engine.WithLayer(pubLayer),
	)

	workflow := NewWorkflowRunner(runner, cmdLayer, pubLayer)

	script := `
        function test_workflow()
            local cmd = command.new("test_command")
            local resp = cmd:response()
            local result = resp:receive()
            return result
        end
    `

	err = vm.Import(script, "test", "test_workflow")
	require.NoError(t, err)

	err = workflow.Start(context.Background(), "test_workflow")
	require.NoError(t, err)

	// First step should give us the command
	cmds, err := workflow.Step()
	require.NoError(t, err)
	require.Len(t, cmds, 1)

	// Process command
	err = workflow.SendResult(cmds[0], lua.LString("TEST"))
	require.NoError(t, err)

	// This step should process the command result and complete
	cmds, err = workflow.Step()
	require.NoError(t, err)
	require.Empty(t, cmds)

	// Check completion - THIS FAILS
	assert.True(t, workflow.IsComplete(), "workflow should be complete")
}
