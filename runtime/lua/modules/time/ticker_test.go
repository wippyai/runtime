package time

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"testing"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestTicker(t *testing.T) {
	logger := zap.NewNop()

	t.Run("ticker basic functionality", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("time", NewTimeModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			function test()
				local results = {}
				local ticker = time.ticker("50ms")

				-- Send 3 ticks
				for i = 1, 3 do
					local t = ticker:channel():receive()
					assert(type(t) == "userdata") -- Should be a time object
					assert(t.hour ~= nil) -- Verify it has time methods
					table.insert(results, "tick")
				end

				ticker:stop()
				return results
			end
		`

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		runner := engine.NewRunner(vm, engine.WithLayer(channel.NewChannelLayer()))

		start := time.Now()
		result, err := runner.Execute(context.Background(), "test")
		duration := time.Since(start)

		require.NoError(t, err)
		resultTable := result.(*lua.LTable)

		// Should receive exactly 3 ticks
		assert.Equal(t, 3, resultTable.Len())
		assert.Equal(t, "tick", resultTable.RawGetInt(1).String())
		assert.Equal(t, "tick", resultTable.RawGetInt(2).String())
		assert.Equal(t, "tick", resultTable.RawGetInt(3).String())

		// Verify timing - should be around 150ms (3 * 50ms)
		assert.GreaterOrEqual(t, duration, 150*time.Millisecond)
		assert.Less(t, duration, 200*time.Millisecond)
	})

	t.Run("ticker with different input types", func(t *testing.T) {
		testCases := []struct {
			name          string
			script        string
			expectError   bool
			errorContains string
		}{
			{
				name: "with duration object",
				script: `
					function test()
						local d = time.parse_duration("100ms")
						local ticker = time.ticker(d)
						ticker:channel():receive()
						ticker:stop()
						return "ok"
					end
				`,
			},
			{
				name: "with string",
				script: `
					function test()
						local ticker = time.ticker("100ms")
						ticker:channel():receive()
						ticker:stop()
						return "ok"
					end
				`,
			},
			{
				name: "with number",
				script: `
					function test()
						local ticker = time.ticker(100)
						ticker:channel():receive()
						ticker:stop()
						return "ok"
					end
				`,
			},
			{
				name: "with invalid duration string",
				script: `
					function test()
						local ticker = time.ticker("invalid")
						return "should not reach here"
					end
				`,
				expectError:   true,
				errorContains: "time: invalid duration",
			},
			{
				name: "with negative duration",
				script: `
					function test()
						local ticker = time.ticker(-100)
						return "should not reach here"
					end
				`,
				expectError:   true,
				errorContains: "duration must be > 0",
			},
			{
				name: "with invalid type",
				script: `
					function test()
						local ticker = time.ticker({})
						return "should not reach here"
					end
				`,
				expectError:   true,
				errorContains: "duration, string, or number expected",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				vm, err := engine.NewCVM(
					logger,
					engine.WithPreloaded("time", NewTimeModule().Loader),
					engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.Import(tc.script, "test", "test")
				require.NoError(t, err)

				runner := engine.NewRunner(vm, engine.WithLayer(channel.NewChannelLayer()))

				result, err := runner.Execute(context.Background(), "test")

				if tc.expectError {
					assert.Error(t, err)
					if tc.errorContains != "" {
						assert.Contains(t, err.Error(), tc.errorContains)
					}
				} else {
					require.NoError(t, err)
					assert.Equal(t, "ok", result.String())
				}
			})
		}
	})

	t.Run("ticker with context cancellation", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("time", NewTimeModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)

		script := `
			function test()
				local ticker = time.ticker("50ms")
				-- Try to receive many times (should be interrupted)
				for i = 1, 100 do
					ticker:channel():receive()
				end
				return "completed"
			end
		`

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		runner := engine.NewRunner(vm, engine.WithLayer(channel.NewChannelLayer()))

		ctx, cancel := context.WithCancel(ctxapi.NewRootContext())

		done := make(chan struct{})
		var execErr error

		go func() {
			defer close(done)
			_, execErr = runner.Execute(ctx, "test")
		}()

		// Let it tick a few times
		time.Sleep(200 * time.Millisecond)
		cancel()

		select {
		case <-done:
			assert.Error(t, execErr)
			assert.Contains(t, execErr.Error(), "context canceled")
		case <-time.After(time.Second):
			t.Fatal("Test didn't complete in time")
		}
	})

	t.Run("ticker with select", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("time", NewTimeModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			function test()
				local results = {}
				local done = channel.new(0)
				local ticker = time.ticker("50ms")

				-- Launch a goroutine to send on a channel after delay
				coroutine.spawn(function()
					time.sleep("125ms")
					done:send("done")
				end)

				-- Collect ticks until done
				while true do
					local result = channel.select{
						ticker:channel():case_receive(),
						done:case_receive()
					}

					if result.channel == done then
						ticker:stop()
						break
					else
						table.insert(results, "tick")
					end
				end

				return results
			end
		`

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		runner := engine.NewRunner(vm,
			engine.WithLayer(channel.NewChannelLayer()),
			engine.WithLayer(coroutine.NewCoroutineLayer()),
		)

		result, err := runner.Execute(context.Background(), "test")
		require.NoError(t, err)

		resultTable := result.(*lua.LTable)

		// Should receive 2-3 ticks (depending on timing)
		count := resultTable.Len()
		assert.GreaterOrEqual(t, count, 2)
		assert.LessOrEqual(t, count, 3)

		// Verify all entries are "tick"
		for i := 1; i <= count; i++ {
			assert.Equal(t, "tick", resultTable.RawGetInt(i).String())
		}
	})
}
