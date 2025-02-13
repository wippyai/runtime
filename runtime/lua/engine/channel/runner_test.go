package channel

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Simple validation layer that enforces a max value rule on yields
type execLayer struct {
	handled [][]lua.LValue
}

func (v *execLayer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	if v.handled == nil {
		v.handled = make([][]lua.LValue, 0)
	}

	// terminate all tests and perform execution here
	for len(tasks) > 0 {
		// Process tasks that have yielded values
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				// Set empty resume values but don't modify the state otherwise
				task.Resumed = []lua.LValue{}
				v.handled = append(v.handled, task.Yielded)
			}
		}
		// Continue the execution chain with all tasks
		nextTasks, err := cvm.Step(tasks...)
		if err != nil {
			return nil, err
		}

		tasks = nextTasks
	}

	return nil, nil
}

func TestChannelRuntimeAsLayer(t *testing.T) {
	logger := zap.NewNop()

	t.Run("channel layer basic operations", func(t *testing.T) {
		// Spawn base CVM
		base, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer base.Close()

		// Spawn wrapped CVM with channel runtime as layer
		channelRuntime := NewChannelLayer()
		wrapped := engine.NewRunner(base,
			engine.WithLayer(channelRuntime),
			engine.WithLayer(&execLayer{}),
		)

		// Test script using channels
		err = base.Import(`
			function test()
				local ch = channel.new(0) -- unbuffered channel
				
				-- Spawn sender
				coroutine.spawn(function()
					coroutine.yield("sender_start")
					ch:send("hello")
					coroutine.yield("sent")
				end)
				
				-- Receive in main routine
				local msg, ok = ch:receive()
				assert(msg == "hello", "wrong message received")
				assert(ok == true, "receive should succeed")
				coroutine.yield("received")
				
				return "done"
			end
		`, "test", "test")
		assert.NoError(t, err)

		// Run and verify
		result, err := wrapped.Execute(context.Background(), "test")
		assert.NoError(t, err)
		if result != nil {
			assert.Equal(t, "done", result.String())
		}
	})

	t.Run("channel layer with multiple operations", func(t *testing.T) {
		base, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer base.Close()

		channelRuntime := NewChannelLayer()
		wrapped := engine.NewRunner(base,
			engine.WithLayer(channelRuntime),
			engine.WithLayer(&execLayer{}),
		)

		err = base.Import(`
			function test()
				local ch1 = channel.new(1) -- buffered
				local ch2 = channel.new(0) -- unbuffered
				local results = {}
	
				-- First goroutine
				coroutine.spawn(function()
					ch1:send("msg1")
					coroutine.yield("ch1_sent")
	
					local msg, ok = ch2:receive()
					assert(msg == "msg2", "wrong message on ch2")
					coroutine.yield("ch2_received")
				end)
	
				-- Second goroutine
				coroutine.spawn(function()
					local msg, ok = ch1:receive()
					assert(msg == "msg1", "wrong message on ch1")
					coroutine.yield("ch1_received")
	
					ch2:send("msg2")
					coroutine.yield("ch2_sent")
				end)
	
				-- wait for all operations to complete
				coroutine.yield("main_waiting")
				return "done"
			end
		`, "test", "test")
		assert.NoError(t, err)

		result, err := wrapped.Execute(context.Background(), "test")
		assert.NoError(t, err)
		assert.Equal(t, "done", result.String())
	})

	t.Run("channel layer cleanup on panic", func(t *testing.T) {
		base, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer base.Close()

		channelRuntime := NewChannelLayer()
		wrapped := engine.NewRunner(base,
			engine.WithLayer(channelRuntime),
			engine.WithLayer(&execLayer{}),
		)

		err = base.Import(`
			function test()
				local ch = channel.new(0)
	
				-- Spawn blocked receiver
				coroutine.spawn(function()
					local msg, ok = ch:receive()
				end)
	
				-- Cause a panic
				error("deliberate panic")
				return "unreachable"
			end
		`, "test", "test")
		assert.NoError(t, err)

		_, err = wrapped.Execute(context.Background(), "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "deliberate panic")
	})
}
