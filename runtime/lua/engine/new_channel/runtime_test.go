package channel

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestBasicChannelOperations(t *testing.T) {
	logger := zap.NewNop()

	// Create VM with channel module
	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	// Push test script
	err = vm.PushScript(`
        -- Create a buffered channel
        local ch = channel.new(1)
        
        -- Sender coroutine
        coroutine.spawn(function()
            ch:send("hello")
            coroutine.yield("sent")
        end)
        
        -- Receiver coroutine
        coroutine.spawn(function()
            local msg, ok = ch:receive()
            assert(msg == "hello", "wrong message")
            assert(ok == true, "expected successful receive")
            coroutine.yield("received")
        end)
    `, "test")
	assert.NoError(t, err)

	// Create runtime and execute
	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	// Process all tasks and collect yields
	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	// Verify both send and receive completed
	assert.Contains(t, yields, "sent")
	assert.Contains(t, yields, "received")
}
