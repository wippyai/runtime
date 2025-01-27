package channel

import (
	"context"
	"testing"

	lua "github.com/yuin/gopher-lua"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestChannelPassingSimple(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
		engine.WithGlobalFunction("new_named", func(l *lua.LState) int {
			name := l.CheckString(1)
			capacity := l.OptInt(2, 0)
			if capacity < 0 {
				l.RaiseError("channel capacity must be >= 0")
				return 0
			}
			l.Push(Wrap(l, Named(name, capacity)))
			return 1
		}),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		-- Create test channels
		local passCh = channel.new(0)    -- channel for passing other channels
		local done = channel.new(0)      -- synchronization
		local namedCh = new_named("test", 0)

		-- Test passing regular channel
		coroutine.spawn(function()
			local ch = channel.new(0)    -- Create regular channel
			passCh:send(ch)              -- Pass it
			ch:send("hello")             
			coroutine.yield("regular_sent")
		end)

		-- Test passing named channel
		coroutine.spawn(function()
			passCh:send(namedCh)         -- Pass named channel
			coroutine.yield("named_sent")
		end)

		-- Receiver for both channels
		coroutine.spawn(function()
			-- Receive and use regular channel
			local ch1 = passCh:receive()
			local msg = ch1:receive()
			assert(msg == "hello", "wrong message: " .. tostring(msg))
			coroutine.yield("regular_received")

			-- Receive named channel
			local ch2 = passCh:receive()
			assert(ch2 == namedCh, "received wrong named channel")
			coroutine.yield("named_received")

			done:send(true)
		end)

		-- wait for completion
		done:receive()
		coroutine.yield("test_done")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelLayer()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{
		"regular_sent",
		"regular_received",
		"named_sent", // post-send, it's ok
		"named_received",
		"test_done",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}
