package time

//
// func TestTimer(t *testing.T) {
//	logger := zap.NewNop()
//
//	t.Run("timer basic functionality", func(t *testing.T) {
//		vm, err := engine.NewCVM(
//			logger,
//			engine.WithPreloaded("time", NewTimeModule().Loader),
//			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
//		)
//		require.NoError(t, err)
//		defer vm.Close()
//
//		script := `
//			function test()
//				local t = time.timer("100ms")
//				t:channel():receive()
//				return "ok"
//			end
//		`
//
//		err = vm.Import(script, "test", "test")
//		require.NoError(t, err)
//
//		channels := channel.NewChannelLayer()
//		asyncRunner := channel.NewAsyncLayer(channels, 4096)
//		wrapped := engine.NewRunner(vm,
//			engine.WithLayer(asyncRunner),
//			engine.WithLayer(channels),
//		)
//
//		ctx := asyncRunner.WithContext(ctxapi.NewRootContext())
//
//		start := time.Now()
//		result, err := wrapped.Execute(ctx, "test")
//		duration := time.Since(start)
//
//		require.NoError(t, err)
//		assert.Equal(t, "ok", result.String())
//		assert.GreaterOrEqual(t, duration, 100*time.Millisecond)
//		assert.Less(t, duration, 150*time.Millisecond)
//	})
//
//	t.Run("timer stop then reset", func(t *testing.T) {
//		vm, err := engine.NewCVM(
//			logger,
//			engine.WithPreloaded("time", NewTimeModule().Loader),
//			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
//		)
//		require.NoError(t, err)
//		defer vm.Close()
//
//		script := `
//			function test()
//				local results = {}
//				local t = time.timer("100ms")
//
//				-- stop then reset
//				t:stop()
//				local reset_result = t:reset("50ms")
//				table.insert(results, reset_result)
//
//				-- wait for timer
//				t:channel():receive()
//				table.insert(results, "fired")
//
//				return results
//			end
//		`
//
//		err = vm.Import(script, "test", "test")
//		require.NoError(t, err)
//
//		channels := channel.NewChannelLayer()
//		asyncRunner := channel.NewAsyncLayer(channels, 4096)
//		wrapped := engine.NewRunner(vm,
//			engine.WithLayer(asyncRunner),
//			engine.WithLayer(channels),
//		)
//
//		ctx := asyncRunner.WithContext(ctxapi.NewRootContext())
//
//		result, err := wrapped.Execute(ctx, "test")
//		require.NoError(t, err)
//
//		resultTable := result.(*lua.LTable)
//		assert.Equal(t, "false", resultTable.RawGetInt(1).String()) // reset returns false after stop
//		assert.Equal(t, "fired", resultTable.RawGetInt(2).String()) // but timer still fires
//	})
//
//	t.Run("timer with stop", func(t *testing.T) {
//		vm, err := engine.NewCVM(
//			logger,
//			engine.WithPreloaded("time", NewTimeModule().Loader),
//			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
//		)
//		require.NoError(t, err)
//		defer vm.Close()
//
//		script := `
//			function test()
//				local t = time.timer("1s")
//				local result = t:stop()
//				return result
//			end
//		`
//
//		err = vm.Import(script, "test", "test")
//		require.NoError(t, err)
//
//		channels := channel.NewChannelLayer()
//		asyncRunner := channel.NewAsyncLayer(channels, 4096)
//		wrapped := engine.NewRunner(vm,
//			engine.WithLayer(asyncRunner),
//			engine.WithLayer(channels),
//		)
//
//		ctx := asyncRunner.WithContext(ctxapi.NewRootContext())
//
//		result, err := wrapped.Execute(ctx, "test")
//		require.NoError(t, err)
//		assert.Equal(t, "true", result.String())
//	})
//
//	t.Run("timer with reset", func(t *testing.T) {
//		vm, err := engine.NewCVM(
//			logger,
//			engine.WithPreloaded("time", NewTimeModule().Loader),
//			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
//		)
//		require.NoError(t, err)
//		defer vm.Close()
//
//		script := `
//			function test()
//				local results = {}
//				local t = time.timer("100ms")
//
//				-- Reset without stopping first
//				local reset_result = t:reset("50ms")
//				table.insert(results, reset_result)
//
//				-- wait for timer
//				t:channel():receive()
//				table.insert(results, "fired")
//
//				return results
//			end
//		`
//
//		err = vm.Import(script, "test", "test")
//		require.NoError(t, err)
//
//		channels := channel.NewChannelLayer()
//		asyncRunner := channel.NewAsyncLayer(channels, 4096)
//		wrapped := engine.NewRunner(vm,
//			engine.WithLayer(asyncRunner),
//			engine.WithLayer(channels),
//		)
//
//		ctx := asyncRunner.WithContext(ctxapi.NewRootContext())
//
//		start := time.Now()
//		result, err := wrapped.Execute(ctx, "test")
//		duration := time.Since(start)
//
//		require.NoError(t, err)
//		resultTable := result.(*lua.LTable)
//		assert.Equal(t, "true", resultTable.RawGetInt(1).String())  // reset successful
//		assert.Equal(t, "fired", resultTable.RawGetInt(2).String()) // timer fired
//		assert.GreaterOrEqual(t, duration, 50*time.Millisecond)
//		assert.Less(t, duration, 100*time.Millisecond)
//	})
//
//	t.Run("timer with different input types", func(t *testing.T) {
//		testCases := []struct {
//			name          string
//			script        string
//			expectError   bool
//			errorContains string
//			minDuration   time.Duration
//		}{
//			{
//				name: "with duration object",
//				script: `
//					function test()
//						local d = time.parse_duration("100ms")
//						local t = time.timer(d)
//						t:channel():receive()
//						return "ok"
//					end
//				`,
//				minDuration: 100 * time.Millisecond,
//			},
//			{
//				name: "with string",
//				script: `
//					function test()
//						local t = time.timer("100ms")
//						t:channel():receive()
//						return "ok"
//					end
//				`,
//				minDuration: 100 * time.Millisecond,
//			},
//			{
//				name: "with number",
//				script: `
//					function test()
//						local t = time.timer(100)
//						t:channel():receive()
//						return "ok"
//					end
//				`,
//				minDuration: 100 * time.Millisecond,
//			},
//			{
//				name: "with invalid duration string",
//				script: `
//					function test()
//						local t = time.timer("invalid")
//						return "should not reach here"
//					end
//				`,
//				expectError:   true,
//				errorContains: "time: invalid duration",
//			},
//			{
//				name: "with negative duration",
//				script: `
//					function test()
//						local t = time.timer(-100)
//						return "should not reach here"
//					end
//				`,
//				expectError:   true,
//				errorContains: "duration must be > 0",
//			},
//			{
//				name: "with invalid type",
//				script: `
//					function test()
//						local t = time.timer({})
//						return "should not reach here"
//					end
//				`,
//				expectError:   true,
//				errorContains: "duration, string, or number expected",
//			},
//		}
//
//		for _, tc := range testCases {
//			t.Run(tc.name, func(t *testing.T) {
//				vm, err := engine.NewCVM(
//					logger,
//					engine.WithPreloaded("time", NewTimeModule().Loader),
//					engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
//				)
//				require.NoError(t, err)
//				defer vm.Close()
//
//				err = vm.Import(tc.script, "test", "test")
//				require.NoError(t, err)
//
//				channels := channel.NewChannelLayer()
//				asyncRunner := channel.NewAsyncLayer(channels, 4096)
//				wrapped := engine.NewRunner(vm,
//					engine.WithLayer(asyncRunner),
//					engine.WithLayer(channels),
//				)
//
//				ctx := asyncRunner.WithContext(ctxapi.NewRootContext())
//
//				start := time.Now()
//				result, err := wrapped.Execute(ctx, "test")
//
//				if tc.expectError {
//					assert.Error(t, err)
//					if tc.errorContains != "" {
//						assert.Contains(t, err.Error(), tc.errorContains)
//					}
//				} else {
//					require.NoError(t, err)
//					assert.Equal(t, "ok", result.String())
//					duration := time.Since(start)
//					assert.GreaterOrEqual(t, duration, tc.minDuration)
//					assert.Less(t, duration, tc.minDuration+50*time.Millisecond)
//				}
//			})
//		}
//	})
//
//	t.Run("timer with context cancellation", func(t *testing.T) {
//		vm, err := engine.NewCVM(
//			logger,
//			engine.WithPreloaded("time", NewTimeModule().Loader),
//			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
//		)
//		require.NoError(t, err)
//		defer vm.Close()
//
//		script := `
//			function test()
//				local t = time.timer("500ms")
//				t:channel():receive()
//				return "completed"
//			end
//		`
//
//		err = vm.Import(script, "test", "test")
//		require.NoError(t, err)
//
//		channels := channel.NewChannelLayer()
//		asyncRunner := channel.NewAsyncLayer(channels, 4096)
//		wrapped := engine.NewRunner(vm,
//			engine.WithLayer(asyncRunner),
//			engine.WithLayer(channels),
//		)
//
//		ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
//		ctx = asyncRunner.WithContext(ctx)
//
//		done := make(chan struct{})
//		var execErr error
//
//		go func() {
//			defer release(done)
//			_, execErr = wrapped.Execute(ctx, "test")
//		}()
//
//		time.Sleep(100 * time.Millisecond)
//		cancel()
//
//		select {
//		case <-done:
//			assert.Error(t, execErr)
//			assert.Contains(t, execErr.Error(), "context canceled")
//		case <-time.After(time.Second):
//			t.Fatal("Test didn't complete in time")
//		}
//	})
//}
//
// func TestTimerSelectAndCoroutine(t *testing.T) {
//	logger := zap.NewNop()
//
//	t.Run("timer with select and coroutine", func(t *testing.T) {
//		vm, err := engine.NewCVM(
//			logger,
//			engine.WithPreloaded("time", NewTimeModule().Loader),
//			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
//		)
//		require.NoError(t, err)
//		defer vm.Close()
//
//		script := `
//			function test()
//				local results = {}
//				local done = channel.new(0)
//
//				-- Launch timer in coroutine
//				coroutine.spawn(function()
//					local t = time.timer("100ms")
//					local ch = channel.new(0)
//
//					-- send on separate channel after 50ms
//					coroutine.spawn(function()
//						time.sleep("50ms")
//						ch:send("message")
//					end)
//
//					-- Select between timer and channel
//					local result = channel.select{
//						t:channel():case_receive(),
//						ch:case_receive()
//					}
//
//					if result.channel == t:channel() then
//						table.insert(results, "timer")
//					else
//						table.insert(results, "channel")
//					end
//
//					-- Second select with reset timer
//					t:reset("50ms")
//					result = channel.select{
//						t:channel():case_receive(),
//						ch:case_receive()
//					}
//
//					if result.channel == t:channel() then
//						table.insert(results, "timer2")
//					else
//						table.insert(results, "channel2")
//					end
//
//					done:send(true)
//				end)
//
//				-- wait for test completion
//				done:receive()
//				return results
//			end
//		`
//
//		err = vm.Import(script, "test", "test")
//		require.NoError(t, err)
//
//		channels := channel.NewChannelLayer()
//		asyncRunner := channel.NewAsyncLayer(channels, 4096)
//		wrapped := engine.NewRunner(vm,
//			engine.WithLayer(channels),
//			engine.WithLayer(asyncRunner),
//			engine.WithLayer(coroutine.NewCoroutineLayer()),
//		)
//
//		ctx := asyncRunner.WithContext(ctxapi.NewRootContext())
//
//		start := time.Now()
//		result, err := wrapped.Execute(ctx, "test")
//		duration := time.Since(start)
//
//		require.NoError(t, err)
//		resultTable := result.(*lua.LTable)
//
//		// Check order of events
//		assert.Equal(t, "channel", resultTable.RawGetInt(1).String(), "Channel should receive before timer")
//		assert.Equal(t, "timer2", resultTable.RawGetInt(2).String(), "Timer should fire after reset")
//
//		// Verify timing
//		assert.GreaterOrEqual(t, duration, 100*time.Millisecond)
//		assert.Less(t, duration, 150*time.Millisecond)
//	})
//}
//
// func TestTimerSelectAndCoroutineInversedOrder(t *testing.T) {
//	logger := zap.NewNop()
//
//	t.Run("timer with select and coroutine", func(t *testing.T) {
//		vm, err := engine.NewCVM(
//			logger,
//			engine.WithPreloaded("time", NewTimeModule().Loader),
//			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
//		)
//		require.NoError(t, err)
//		defer vm.Close()
//
//		script := `
//			function test()
//				local results = {}
//				local done = channel.new(0)
//
//				-- Launch timer in coroutine
//				coroutine.spawn(function()
//					local t = time.timer("100ms")
//					local ch = channel.new(0)
//
//					-- send on separate channel after 50ms
//					coroutine.spawn(function()
//						time.sleep("50ms")
//						ch:send("message")
//					end)
//
//					-- Select between timer and channel
//					local result = channel.select{
//						t:channel():case_receive(),
//						ch:case_receive()
//					}
//
//					if result.channel == t:channel() then
//						table.insert(results, "timer")
//					else
//						table.insert(results, "channel")
//					end
//
//					-- Second select with reset timer
//					t:reset("50ms")
//					result = channel.select{
//						t:channel():case_receive(),
//						ch:case_receive()
//					}
//
//					if result.channel == t:channel() then
//						table.insert(results, "timer2")
//					else
//						table.insert(results, "channel2")
//					end
//
//					done:send(true)
//				end)
//
//				-- wait for test completion
//				done:receive()
//				return results
//			end
//		`
//
//		err = vm.Import(script, "test", "test")
//		require.NoError(t, err)
//
//		channels := channel.NewChannelLayer()
//		asyncRunner := channel.NewAsyncLayer(channels, 4096)
//		wrapped := engine.NewRunner(vm,
//			engine.WithLayer(coroutine.NewCoroutineLayer()),
//			engine.WithLayer(channels),
//			engine.WithLayer(asyncRunner),
//		)
//
//		ctx := asyncRunner.WithContext(ctxapi.NewRootContext())
//
//		start := time.Now()
//		result, err := wrapped.Execute(ctx, "test")
//		duration := time.Since(start)
//
//		require.NoError(t, err)
//		resultTable := result.(*lua.LTable)
//
//		// Check order of events
//		assert.Equal(t, "channel", resultTable.RawGetInt(1).String(), "Channel should receive before timer")
//		assert.Equal(t, "timer2", resultTable.RawGetInt(2).String(), "Timer should fire after reset")
//
//		// Verify timing
//		assert.GreaterOrEqual(t, duration, 100*time.Millisecond)
//		assert.Less(t, duration, 150*time.Millisecond)
//	})
//}
