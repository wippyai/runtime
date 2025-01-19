package async

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"
)

func TestChromiseTimeBasic(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("chromise", NewChromiseModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	tg := engine.NewTaskGroup(100)
	ctx := engine.WithTaskGroup(context.Background(), tg)
	ctx = WithScheduleChannel(ctx, make(chan scheduleItem, 100))

	vm.SetContext(ctx)

	err = vm.Import(`
		function test()
			local t = chromise.time_after(50)
			t:receive()
			return "ok"
		end
    `, "test", "test")
	assert.NoError(t, err)

	channels := channel.NewChannelRunner()
	chromise := NewAsyncRunner(channels)

	wrapped := engine.NewWrappedCVM(vm,
		engine.WithLayer(chromise),
		engine.WithLayer(channels),
	)

	start := time.Now()
	result, err := wrapped.Execute(ctx, "test")
	duration := time.Since(start)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "ok", result.String())

	assert.GreaterOrEqual(t, duration, 50*time.Millisecond)
	assert.Less(t, duration, 100*time.Millisecond)
}

func TestChromiseRunner(t *testing.T) {
	t.Run("concurrent timers", func(t *testing.T) {
		logger := zap.NewNop()
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("chromise", NewChromiseModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		tg := engine.NewTaskGroup(100)
		ctx := engine.WithTaskGroup(context.Background(), tg)
		ctx = WithScheduleChannel(ctx, make(chan scheduleItem, 100))
		vm.SetContext(ctx)

		script := `
			function test_concurrent()
				local results = {}
				local done = channel.new(0)

				coroutine.spawn(function()
					local t1 = chromise.time_after(50)
					t1:receive()
					table.insert(results, "timer1")
					if #results == 3 then
						done:send(true)
					end
				end)

				coroutine.spawn(function()
					local t2 = chromise.time_after(100)
					t2:receive()
					table.insert(results, "timer2")
					if #results == 3 then
						done:send(true)
					end
				end)

				coroutine.spawn(function()
					local t3 = chromise.time_after(150)
					t3:receive()
					table.insert(results, "timer3")
					if #results == 3 then
						done:send(true)
					end
				end)

				done:receive()
				return results
			end
		`

		err = vm.Import(script, "test", "test_concurrent")
		require.NoError(t, err)

		channels := channel.NewChannelRunner()
		chromise := NewAsyncRunner(channels)
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(chromise),
			engine.WithLayer(channels),
		)

		start := time.Now()
		result, err := wrapped.Execute(ctx, "test_concurrent")
		duration := time.Since(start)

		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify it's a table
		resultTable, ok := result.(*lua.LTable)
		require.True(t, ok, "expected result to be a table")

		// Check values in order of completion
		assert.Equal(t, "timer1", resultTable.RawGetInt(1).String())
		assert.Equal(t, "timer2", resultTable.RawGetInt(2).String())
		assert.Equal(t, "timer3", resultTable.RawGetInt(3).String())

		assert.GreaterOrEqual(t, duration, 150*time.Millisecond)
		assert.Less(t, duration, 200*time.Millisecond)
	})

	t.Run("timer cancellation", func(t *testing.T) {
		logger := zap.NewNop()
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("chromise", NewChromiseModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		tg := engine.NewTaskGroup(100)
		ctx, cancel := context.WithCancel(context.Background())
		ctx = engine.WithTaskGroup(ctx, tg)
		ctx = WithScheduleChannel(ctx, make(chan scheduleItem, 100))
		vm.SetContext(ctx)

		script := `
			function test_cancel()
				local t = chromise.time_after(1000)
				local result = t:receive()
				return result
			end
		`

		err = vm.Import(script, "test", "test_cancel")
		require.NoError(t, err)

		channels := channel.NewChannelRunner()
		chromise := NewAsyncRunner(channels)
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(chromise),
			engine.WithLayer(channels),
		)

		var wg sync.WaitGroup
		wg.Add(1)

		// Start execution in a goroutine
		var execErr error
		go func() {
			defer wg.Done()
			_, execErr = wrapped.Execute(ctx, "test_cancel")
		}()

		// Cancel context after a short delay
		cancel()
		wg.Wait()

		assert.Error(t, execErr)
		assert.Contains(t, execErr.Error(), "context canceled")
	})

	t.Run("error propagation", func(t *testing.T) {
		logger := zap.NewNop()
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("chromise", NewChromiseModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		tg := engine.NewTaskGroup(100)
		ctx := engine.WithTaskGroup(context.Background(), tg)
		ctx = WithScheduleChannel(ctx, make(chan scheduleItem, 100))
		vm.SetContext(ctx)

		script := `
			function test_error()
				local t = chromise.time_after(-1)  -- Invalid duration
				return t:receive()
			end
		`

		err = vm.Import(script, "test", "test_error")
		require.NoError(t, err)

		channels := channel.NewChannelRunner()
		chromise := NewAsyncRunner(channels)
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(chromise),
			engine.WithLayer(channels),
		)

		_, err = wrapped.Execute(ctx, "test_error")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duration must be > 0")
	})
}
