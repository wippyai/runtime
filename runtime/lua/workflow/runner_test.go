package runtime

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/command"
	"github.com/ponyruntime/pony/runtime/lua/engine/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"log"
	"testing"
)

func TestWorkflowRunner_BasicFlow(t *testing.T) {
	logger := zap.NewNop()

	// Create VM with required modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("command", command.NewCommandModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Create layers
	channels := channel.NewChannelLayer()
	cmdLayer := command.NewCommandLayer(channels)
	pubLayer := pubsub.NewSubscriptionLayer(channels)

	// Create runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(cmdLayer),
		engine.WithLayer(pubLayer),
	)

	// Create workflow runner
	workflow := NewWorkflowRunner(runner, cmdLayer, pubLayer)

	// Define test script
	script := `
        function test_workflow()
            -- Create a command to process
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

	// Start workflow
	err = workflow.Start(context.Background(), "test_workflow")
	require.NoError(t, err)

	var processedCmd *command.Command

	// Run workflow until completion
	for !workflow.IsComplete() {
		cmds, err := workflow.Step()

		require.NoError(t, err)

		log.Printf("cmds: %v\n", cmds)

		// Process commands if any
		for _, cmd := range cmds {
			// Verify command type and value
			assert.Equal(t, command.Type("test_command"), cmd.CmdType())
			processedCmd = cmd
		}

		// Set command result if we have a processed command
		if processedCmd != nil {
			err = workflow.SetCommandResult(processedCmd, lua.LString("hello"))
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
