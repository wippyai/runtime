package timer

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestTimerLayer(t *testing.T) {
	logger := zap.NewNop()

	t.Run("basic timer operation", func(t *testing.T) {
		// Create base VM
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
			engine.WithPreloaded("timer", NewTimerModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		// Setup context with timer context
		ctx := WithTimerContext(context.Background())
		vm.SetContext(ctx)

		// Create wrapped VM with channel and timer layers
		channelRunner := channel.NewChannelRunner()
		timerLayer := NewTimerLayer(channelRunner)

		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(channelRunner),
			engine.WithLayer(timerLayer),
		)

		// Test script
		err = vm.Import(`
            function test()
                local start = os.time()
                
                -- Create a timer for 100ms
                local ch = timer.sleep(0.1)
                
                -- Wait for timer completion
                local _, ok = ch:receive()
                assert(ok, "timer channel should complete successfully")
                
                local duration = os.time() - start
                assert(duration >= 0, "timer should take at least 0.1 seconds")
                
                return "done"
            end
        `, "test", "test")
		assert.NoError(t, err)

		// Execute and verify
		result, err := wrapped.Execute(ctx, "test")
		assert.NoError(t, err)
		assert.Equal(t, "done", result.String())
	})

	t.Run("multiple concurrent timers", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
			engine.WithPreloaded("timer", NewTimerModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		ctx := WithTimerContext(context.Background())
		vm.SetContext(ctx)

		channelRunner := channel.NewChannelRunner()
		timerLayer := NewTimerLayer(channelRunner)

		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(channelRunner),
			engine.WithLayer(timerLayer),
		)

		err = vm.Import(`
            function test()
                -- Start multiple timers
                local ch1 = timer.after(0.1)
                local ch2 = timer.after(0.2)
                
                -- Wait for both timers
                local results = {}
                
                local function await(ch)
                    local val, ok = ch:receive()
                    return ok
                end
                
                -- Spawn coroutines to wait for timers
                coroutine.spawn(function()
                    results[1] = await(ch1)
                    coroutine.yield("timer1_complete")
                end)
                
                coroutine.spawn(function()
                    results[2] = await(ch2)
                    coroutine.yield("timer2_complete")
                end)
                
                -- Ensure both timers completed
                assert(results[1], "timer 1 should complete")
                assert(results[2], "timer 2 should complete")
                
                return "all_done"
            end
        `, "test", "test")
		assert.NoError(t, err)

		result, err := wrapped.Execute(ctx, "test")
		assert.NoError(t, err)
		assert.Equal(t, "all_done", result.String())
	})
}
