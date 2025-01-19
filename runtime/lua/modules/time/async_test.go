package time

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestTimeAfter(t *testing.T) {
	logger := zap.NewNop()

	t.Run("after with different input types", func(t *testing.T) {
		testCases := []struct {
			name          string
			script        string
			expectError   bool
			errorContains string
			minDuration   time.Duration
		}{
			{
				name: "with duration object",
				script: `
					function test()
						local d = time.parse_duration("100ms")
						local t = time.after(d)
						t:receive()
						return "ok"
					end
				`,
				minDuration: 100 * time.Millisecond,
			},
			{
				name: "with string",
				script: `
					function test()
						local t = time.after("100ms")
						t:receive()
						return "ok"
					end
				`,
				minDuration: 100 * time.Millisecond,
			},
			{
				name: "with number (milliseconds)",
				script: `
					function test()
						local t = time.after(100)
						t:receive()
						return "ok"
					end
				`,
				minDuration: 100 * time.Millisecond,
			},
			{
				name: "with invalid string",
				script: `
					function test()
						local t = time.after("invalid")
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
						local t = time.after(-100)
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
						local t = time.after({})
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

				tg := engine.NewTaskGroup(100)
				ctx := engine.WithTaskGroup(context.Background(), tg)
				ctx = async.WithAsyncChannel(ctx)
				vm.SetContext(ctx)

				err = vm.Import(tc.script, "test", "test")
				require.NoError(t, err)

				channels := channel.NewChannelRunner()
				asyncRunner := async.NewAsyncRunner(channels)
				wrapped := engine.NewWrappedCVM(vm,
					engine.WithLayer(asyncRunner),
					engine.WithLayer(channels),
				)

				start := time.Now()
				result, err := wrapped.Execute(ctx, "test")

				if tc.expectError {
					assert.Error(t, err)
					if tc.errorContains != "" {
						assert.Contains(t, err.Error(), tc.errorContains)
					}
				} else {
					require.NoError(t, err)
					assert.Equal(t, "ok", result.String())
					duration := time.Since(start)
					assert.GreaterOrEqual(t, duration, tc.minDuration)
					assert.Less(t, duration, tc.minDuration+50*time.Millisecond)
				}
			})
		}
	})
}

func TestTimers(t *testing.T) {
	logger := zap.NewNop()

	t.Run("concurrent timers", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("time", NewTimeModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		tg := engine.NewTaskGroup(100)
		ctx := engine.WithTaskGroup(context.Background(), tg)
		ctx = async.WithAsyncChannel(ctx)
		vm.SetContext(ctx)

		script := `
            function test()
                local results = {}
                local done = channel.new(0)

                -- Start three timers with different durations
                coroutine.spawn(function()
                    local t1 = time.after("50ms")
                    t1:receive()
                    table.insert(results, "timer1")
                    if #results == 3 then
                        done:send(true)
                    end
                end)

                coroutine.spawn(function()
                    local t2 = time.after("100ms")
                    t2:receive()
                    table.insert(results, "timer2")
                    if #results == 3 then
                        done:send(true)
                    end
                end)

                coroutine.spawn(function()
                    local t3 = time.after("150ms")
                    t3:receive()
                    table.insert(results, "timer3")
                    if #results == 3 then
                        done:send(true)
                    end
                end)

                -- Wait for all timers to complete
                done:receive()
                return results
            end
        `

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelRunner()
		asyncRunner := async.NewAsyncRunner(channels)
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)

		start := time.Now()
		result, err := wrapped.Execute(ctx, "test")
		duration := time.Since(start)
		require.NoError(t, err)

		// Verify result order
		resultTable := result.(*lua.LTable)
		assert.Equal(t, "timer1", resultTable.RawGetInt(1).String(), "First timer should complete first")
		assert.Equal(t, "timer2", resultTable.RawGetInt(2).String(), "Second timer should complete second")
		assert.Equal(t, "timer3", resultTable.RawGetInt(3).String(), "Third timer should complete third")

		// Verify timing
		assert.GreaterOrEqual(t, duration, 150*time.Millisecond, "Should take at least 150ms")
		assert.Less(t, duration, 200*time.Millisecond, "Should not take too long")
	})

	t.Run("timer cancellation", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("time", NewTimeModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		tg := engine.NewTaskGroup(100)
		ctx, cancel := context.WithCancel(context.Background())
		ctx = engine.WithTaskGroup(ctx, tg)
		ctx = async.WithAsyncChannel(ctx)
		vm.SetContext(ctx)

		script := `
            function test()
                local t1 = time.after("500ms")
                -- Simple receive, no pcall needed as context cancellation
                -- will be caught by the Go layer
                t1:receive()
                return "completed"
            end
        `

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelRunner()
		asyncRunner := async.NewAsyncRunner(channels)
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)

		// Start execution in a goroutine
		done := make(chan struct{})
		var execErr error

		go func() {
			defer close(done)
			_, execErr = wrapped.Execute(ctx, "test")
		}()

		// Wait a bit then cancel
		time.Sleep(100 * time.Millisecond)
		cancel()

		// Wait for completion or timeout
		select {
		case <-done:
			assert.Error(t, execErr)
			assert.Contains(t, execErr.Error(), "context canceled")
		case <-time.After(time.Second):
			t.Fatal("Test didn't complete in time")
		}
	})

	t.Run("sequential timer reuse", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("time", NewTimeModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		tg := engine.NewTaskGroup(100)
		ctx := engine.WithTaskGroup(context.Background(), tg)
		ctx = async.WithAsyncChannel(ctx)
		vm.SetContext(ctx)

		script := `
            function test()
                local results = {}
                
                -- Use same duration multiple times sequentially
                for i = 1, 3 do
                    local t = time.after("50ms")
                    t:receive()
                    table.insert(results, "timer" .. i)
                end
                
                return results
            end
        `

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelRunner()
		asyncRunner := async.NewAsyncRunner(channels)
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)

		start := time.Now()
		result, err := wrapped.Execute(ctx, "test")
		duration := time.Since(start)
		require.NoError(t, err)

		// Verify results
		resultTable := result.(*lua.LTable)
		assert.Equal(t, "timer1", resultTable.RawGetInt(1).String())
		assert.Equal(t, "timer2", resultTable.RawGetInt(2).String())
		assert.Equal(t, "timer3", resultTable.RawGetInt(3).String())

		// Verify timing (should be roughly 150ms for three sequential 50ms timers)
		assert.GreaterOrEqual(t, duration, 150*time.Millisecond)
		assert.Less(t, duration, 200*time.Millisecond)
	})

	t.Run("mixed timer durations", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("time", NewTimeModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		tg := engine.NewTaskGroup(100)
		ctx := engine.WithTaskGroup(context.Background(), tg)
		ctx = async.WithAsyncChannel(ctx)
		vm.SetContext(ctx)

		script := `
            function test()
                local results = {}
                local done = channel.new(0)
                
                -- Mix string durations, numbers, and parsed durations
                coroutine.spawn(function()
                    local t1 = time.after("75ms")
                    t1:receive()
                    table.insert(results, "string_duration")
                    if #results == 3 then done:send(true) end
                end)
                
                coroutine.spawn(function()
                    local t2 = time.after(50)  -- 50ms as number
                    t2:receive()
                    table.insert(results, "number_duration")
                    if #results == 3 then done:send(true) end
                end)
                
                coroutine.spawn(function()
                    local d = time.parse_duration("100ms")
                    local t3 = time.after(d)
                    t3:receive()
                    table.insert(results, "parsed_duration")
                    if #results == 3 then done:send(true) end
                end)
                
                done:receive()
                return results
            end
        `

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelRunner()
		asyncRunner := async.NewAsyncRunner(channels)
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)

		start := time.Now()
		result, err := wrapped.Execute(ctx, "test")
		duration := time.Since(start)
		require.NoError(t, err)

		// Verify all timers completed
		resultTable := result.(*lua.LTable)
		assert.Equal(t, 3, resultTable.Len())

		// The order should be: number_duration (50ms), string_duration (75ms), parsed_duration (100ms)
		assert.Equal(t, "number_duration", resultTable.RawGetInt(1).String())
		assert.Equal(t, "string_duration", resultTable.RawGetInt(2).String())
		assert.Equal(t, "parsed_duration", resultTable.RawGetInt(3).String())

		// Verify timing
		assert.GreaterOrEqual(t, duration, 100*time.Millisecond)
		assert.Less(t, duration, 150*time.Millisecond)
	})
}
