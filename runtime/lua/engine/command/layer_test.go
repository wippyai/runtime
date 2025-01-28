package command

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
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

	// Execute and collect yields
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

		// Get next batch of tasks through task group
		group, err := runner.GetTaskGroup().Wait(ctx, vm, false)
		assert.NoError(t, err)
		tasks, err = runner.Step(append(group, tasks...)...)
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
