package timer

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	timemod "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func createTestVM(t *testing.T) *engine.CoroutineVM {
	logger := zap.NewNop()
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("timer", NewTimerModule().Loader),
		engine.WithPreloaded("time", timemod.NewTimeModule().Loader),
	)
	assert.NoError(t, err)
	return vm
}

func TestTimerWithDurations(t *testing.T) {
	t.Run("timer with duration object", func(t *testing.T) {
		vm := createTestVM(t)
		defer vm.Close()

		ctx := WithTimerContext(context.Background())
		vm.SetContext(ctx)

		channelRunner := channel.NewChannelRunner()
		timerLayer := NewTimerLayer(channelRunner)

		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(channelRunner),
			engine.WithLayer(timerLayer),
		)

		err := vm.Import(`
            function test()
                -- Create a duration of 100ms
                local duration = time.parse_duration("100ms")
                assert(duration ~= nil, "duration should be valid")
                
                -- Use duration with timer
                local ch = timer.sleep(duration)
                local _, ok = ch:receive()
                assert(ok, "timer should complete successfully")
                
                return "done"
            end
        `, "test", "test")
		assert.NoError(t, err)

		result, err := wrapped.Execute(ctx, "test")
		assert.NoError(t, err)
		assert.Equal(t, "done", result.String())
	})

	t.Run("timer with duration string", func(t *testing.T) {
		vm := createTestVM(t)
		defer vm.Close()

		ctx := WithTimerContext(context.Background())
		vm.SetContext(ctx)

		channelRunner := channel.NewChannelRunner()
		timerLayer := NewTimerLayer(channelRunner)

		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(channelRunner),
			engine.WithLayer(timerLayer),
		)

		err := vm.Import(`
            function test()
                -- Use duration string directly
                local ch = timer.sleep("200ms")
                local _, ok = ch:receive()
                assert(ok, "timer should complete successfully")
                
                return "done"
            end
        `, "test", "test")
		assert.NoError(t, err)

		result, err := wrapped.Execute(ctx, "test")
		assert.NoError(t, err)
		assert.Equal(t, "done", result.String())
	})

	t.Run("multiple timer types", func(t *testing.T) {
		vm := createTestVM(t)
		defer vm.Close()

		ctx := WithTimerContext(context.Background())
		vm.SetContext(ctx)

		channelRunner := channel.NewChannelRunner()
		timerLayer := NewTimerLayer(channelRunner)

		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(channelRunner),
			engine.WithLayer(timerLayer),
		)

		err := vm.Import(`
            function test()
                -- Test multiple timer creation methods
                local results = {}
                
                coroutine.spawn(function()
                    local ch = timer.sleep(0.1)  -- seconds as number
                    ch:receive()
                    results[1] = true
                    coroutine.yield("timer1_done")
                end)
                
                coroutine.spawn(function()
                    local ch = timer.after("150ms")  -- duration string
                    ch:receive()
                    results[2] = true
                    coroutine.yield("timer2_done")
                end)
                
                coroutine.spawn(function()
                    local duration = time.parse_duration("200ms")
                    local ch = timer.timeout(duration)  -- duration object
                    ch:receive()
                    results[3] = true
                    coroutine.yield("timer3_done")
                end)
                
                -- Verify all timers completed
                assert(results[1], "timer 1 should complete")
                assert(results[2], "timer 2 should complete")
                assert(results[3], "timer 3 should complete")
                
                return "all_done"
            end
        `, "test", "test")
		assert.NoError(t, err)

		result, err := wrapped.Execute(ctx, "test")
		assert.NoError(t, err)
		assert.Equal(t, "all_done", result.String())
	})

	t.Run("error handling for invalid durations", func(t *testing.T) {
		vm := createTestVM(t)
		defer vm.Close()

		ctx := WithTimerContext(context.Background())
		vm.SetContext(ctx)

		channelRunner := channel.NewChannelRunner()
		timerLayer := NewTimerLayer(channelRunner)

		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(channelRunner),
			engine.WithLayer(timerLayer),
		)

		err := vm.Import(`
            function test()
                -- Test invalid duration string
                local ok, err = pcall(function()
                    timer.sleep("invalid")
                end)
                assert(not ok, "should fail with invalid duration")
                
                -- Test invalid type
                ok, err = pcall(function()
                    timer.sleep({})
                end)
                assert(not ok, "should fail with invalid type")
                
                return "done"
            end
        `, "test", "test")
		assert.NoError(t, err)

		result, err := wrapped.Execute(ctx, "test")
		assert.NoError(t, err)
		assert.Equal(t, "done", result.String())
	})
}
