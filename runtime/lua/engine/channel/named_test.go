package channel

//
//func TestNamedChannels_SignalAggregation(t *testing.T) {
//	logger := zap.NewNop()
//
//	t.Run("aggregate three parent signals", func(t *testing.T) {
//		scheduler := NewRuntime()
//		channels := NewChannelModule()
//
//		createNamedChannel := func(L *lua.LState) int {
//			name := L.CheckString(1)
//			t.Logf("Creating named channel: %s", name)
//			ch := Named(name)
//			ud := L.NewUserData()
//			ud.Value = ch
//			L.SetMetatable(ud, L.GetTypeMetatable("channel"))
//			L.Push(ud)
//			return 1
//		}
//
//		vm, err := engine.NewCoroutineVM(
//			context.Background(),
//			logger,
//			engine.WithPreloaded(channels.Name(), channels.Loader),
//			engine.WithGlobalFunction("create_named_channel", createNamedChannel),
//		)
//		assert.NoError(t, err)
//		defer vm.Close()
//
//		err = vm.PushScript(`
//            print("Starting script execution")
//            coroutine.go(function()
//                print("Creating channels")
//                local parent1 = create_named_channel("parent1")
//                local parent2 = create_named_channel("parent2")
//                local parent3 = create_named_channel("parent3")
//                print("Channels created")
//
//                local results = {}
//
//                print("Waiting for parent1")
//                coroutine.yield("waiting_parent1")
//                print("About to receive from parent1")
//                local msg1, ok = parent1:receive()
//                print("Received from parent1:", msg1, ok)
//                assert(ok, "failed to receive from parent1")
//                results[1] = msg1
//                coroutine.yield("got_parent1")
//
//                print("About to receive from parent2")
//                local msg2, ok = parent2:receive()
//                print("Received from parent2:", msg2, ok)
//                assert(ok, "failed to receive from parent2")
//                results[2] = msg2
//                coroutine.yield("got_parent2")
//
//                print("About to receive from parent3")
//                local msg3, ok = parent3:receive()
//                print("Received from parent3:", msg3, ok)
//                assert(ok, "failed to receive from parent3")
//                results[3] = msg3
//                coroutine.yield("got_parent3")
//
//                print("Preparing final result")
//                local result = results[1] .. "-" .. results[2] .. "-" .. results[3]
//                print("Final result:", result)
//                coroutine.yield(result)
//            end)
//        `, "test")
//		assert.NoError(t, err)
//
//		// Get initial tasks and validate
//		t.Log("Getting initial tasks")
//		tasks, err := vm.Step()
//		assert.NoError(t, err)
//		t.Logf("Initial tasks count: %d", len(tasks))
//		require.NotEmpty(t, tasks, "should have tasks after initial step")
//
//		if len(tasks) > 0 {
//			t.Logf("First yield value: %v", tasks[0].Yielded[0])
//		}
//
//		// First we should see it waiting for parent1
//		require.Equal(t, "waiting_parent1", tasks[0].Yielded[0].String())
//
//		// Step to get to the receive
//		t.Log("Stepping to receive")
//		tasks, err = scheduler.Step(vm, tasks...)
//		assert.NoError(t, err)
//		t.Logf("Tasks after scheduler step: %d", len(tasks))
//		if len(tasks) > 0 && len(tasks[0].Yielded) > 0 {
//			t.Logf("Yield value after step: %v", tasks[0].Yielded[0])
//		}
//		require.NotEmpty(t, tasks, "should have tasks after initial receive")
//
//		// Check registered channels
//		listeners := scheduler.GetOpenChannels()
//		t.Logf("Registered channels: %v", listeners)
//
//		// Send signals one by one and verify
//		signals := []struct {
//			channel string
//			value   string
//			expect  string
//		}{
//			{"parent1", "signal1", "got_parent1"},
//			{"parent2", "signal2", "got_parent2"},
//			{"parent3", "signal3", "got_parent3"},
//		}
//
//		for i, s := range signals {
//			t.Logf("Processing signal %d: %s -> %s", i, s.channel, s.value)
//
//			// Send signal
//			tasks, err = scheduler.Send(s.channel, lua.LString(s.value))
//			assert.NoError(t, err)
//			t.Logf("Tasks after send: %d", len(tasks))
//			require.NotEmpty(t, tasks, "should have tasks after sending signal")
//
//			// Process the signal
//			tasks, err = scheduler.Step(vm, tasks...)
//			assert.NoError(t, err)
//			t.Logf("Tasks after step: %d", len(tasks))
//			if len(tasks) > 0 && len(tasks[0].Yielded) > 0 {
//				t.Logf("Yield value: %v", tasks[0].Yielded[0])
//			}
//			require.NotEmpty(t, tasks, "should have tasks after step")
//			assert.Equal(t, s.expect, tasks[0].Yielded[0].String())
//
//			// Check channel state
//			listeners = scheduler.GetOpenChannels()
//			t.Logf("Registered channels after signal %d: %v", i, listeners)
//		}
//
//		// Verify final state
//		require.NotEmpty(t, tasks, "should have final task")
//		assert.Equal(t, "signal1-signal2-signal3", tasks[0].Yielded[0].String())
//
//		listeners = scheduler.GetOpenChannels()
//		t.Logf("Final registered channels: %v", listeners)
//		assert.Empty(t, scheduler.GetOpenChannels(), "all channels should be unregistered")
//	})
//}

//
//func TestExternalChannels_Basic(t *testing.T) {
//	logger := zap.NewNop()
//
//	t.Run("inbox channel send data", func(t *testing.T) {
//		bufferedScheduler := NewRuntime()
//		channels := NewChannelModule()
//
//		vm, err := engine.NewCoroutineVM(
//			context.Background(), logger,
//			engine.WithPreloaded(channels.Name(), channels.Loader),
//		)
//
//		if err != nil {
//			t.Fatal(err)
//		}
//		defer vm.Close()
//
//		err = vm.PushScript(`
//        local ch = channel.inbox("signal")
//
//        -- Receiver
//        coroutine.go(function()
//            local msg, ok = ch:receive()
//            assert(ok, "expected successful receive")
//            assert(msg == "hello", "wrong message received")
//            coroutine.yield("receive_complete")
//        end)
//    `, "test")
//
//		if err != nil {
//			t.Fatal(err)
//		}
//
//		// Get initial yielded tasks
//		tasks, err := bufferedScheduler.Step(vm)
//		assert.NoError(t, err)
//
//		log.Printf("---------------------------------------tasks: %v", tasks)
//		// Verify inbox channel is registered
//		listeners := bufferedScheduler.GetOpenChannels()
//		assert.Equal(t, 1, len(listeners), "expected one inbox listener")
//		assert.Equal(t, "signal", listeners[0], "expected signal channel to be registered")
//
//		// send data to inbox channel
//		tasks, _ = bufferedScheduler.Send("signal", lua.LString("hello"))
//		assert.Equal(t, 1, len(tasks), "expected one task to be resumed")
//
//		// Process resumed task
//		tasks, err = bufferedScheduler.Step(vm, tasks...)
//		assert.NoError(t, err)
//		assert.Equal(t, 1, len(tasks), "expected one final yield")
//
//		// Verify channel was unregistered after completion
//		listeners = bufferedScheduler.GetOpenChannels()
//		assert.Equal(t, 0, len(listeners), "expected no remaining listeners")
//	})
//
//	t.Run("inbox channel multi-receive with yield", func(t *testing.T) {
//		bufferedScheduler := NewRuntime()
//		channels := NewChannelModule()
//
//		vm, err := engine.NewCoroutineVM(
//			context.Background(), logger,
//			engine.WithPreloaded(channels.Name(), channels.Loader),
//		)
//		assert.NoError(t, err)
//		defer vm.Close()
//
//		err = vm.PushScript(`
//        local ch = channel.inbox("signal")
//
//        coroutine.go(function()
//            -- First receive
//            local msg1, ok = ch:receive()
//            assert(ok and msg1 == "first", "wrong first message")
//
//            -- Second receive
//            local msg2, ok = ch:receive()
//            assert(ok and msg2 == "second", "wrong second message")
//
//            -- Yield doing something else
//            coroutine.yield("other_work")
//
//            -- Third receive after yield
//            local msg3, ok = ch:receive()
//            assert(ok and msg3 == "third", "wrong third message")
//
//            coroutine.yield("all_done")
//        end)
//    `, "test")
//		assert.NoError(t, err)
//
//		// Get initial task
//		tasks, err := bufferedScheduler.Step(vm)
//		assert.NoError(t, err)
//
//		// First signal
//		tasks, _ = bufferedScheduler.Send("signal", lua.LString("first"))
//		assert.Equal(t, 1, len(tasks))
//		tasks, err = bufferedScheduler.Step(vm, tasks...)
//		assert.NoError(t, err)
//
//		// Second signal
//		tasks, _ = bufferedScheduler.Send("signal", lua.LString("second"))
//		assert.Equal(t, 1, len(tasks))
//		tasks, err = bufferedScheduler.Step(vm, tasks...)
//		assert.NoError(t, err)
//
//		// Should now be yielded with "other_work"
//		assert.Equal(t, 1, len(tasks))
//		assert.Equal(t, "other_work", tasks[0].Yielded[0].String())
//
//		// Verify channel not in listeners during yield
//		assert.Equal(t, 0, len(bufferedScheduler.GetOpenChannels()))
//
//		// Step to get to next receive
//		tasks, err = bufferedScheduler.Step(vm, tasks...)
//		assert.NoError(t, err)
//
//		// Channel should be listening again
//		assert.Equal(t, []string{"signal"}, bufferedScheduler.GetOpenChannels())
//
//		// Third signal
//		tasks, _ = bufferedScheduler.Send("signal", lua.LString("third"))
//		assert.Equal(t, 1, len(tasks))
//		tasks, err = bufferedScheduler.Step(vm, tasks...)
//		assert.NoError(t, err)
//
//		// Should be done
//		assert.Equal(t, "all_done", tasks[0].Yielded[0].String())
//	})
//
//	t.Run("multiple receivers on single inbox channel", func(t *testing.T) {
//		bufferedScheduler := NewRuntime()
//		channels := NewChannelModule()
//
//		vm, err := engine.NewCoroutineVM(
//			context.Background(), logger,
//			engine.WithPreloaded(channels.Name(), channels.Loader),
//		)
//		assert.NoError(t, err)
//		defer vm.Close()
//
//		err = vm.PushScript(`
//            local ch = channel.inbox("distributed")
//
//            -- First receiver
//            coroutine.go(function()
//                local msg, ok = ch:receive()
//                assert(ok and msg == "first", "wrong message in first receiver")
//                coroutine.yield("first_done")
//            end)
//
//            -- Second receiver
//            coroutine.go(function()
//                local msg, ok = ch:receive()
//                assert(ok and msg == "second", "wrong message in second receiver")
//                coroutine.yield("second_done")
//            end)
//        `, "test")
//		assert.NoError(t, err)
//
//		// Get initial tasks
//		tasks, err := bufferedScheduler.Step(vm)
//		assert.NoError(t, err)
//
//		// Verify both are registered
//		assert.Equal(t, []string{"distributed"}, bufferedScheduler.GetOpenChannels())
//
//		// send first signal - should go to first receiver
//		tasks, _ = bufferedScheduler.Send("distributed", lua.LString("first"))
//		assert.Equal(t, 1, len(tasks), "first receiver should be resumed")
//
//		tasks, err = bufferedScheduler.Step(vm, tasks...)
//		assert.NoError(t, err)
//		assert.Equal(t, "first_done", tasks[0].Yielded[0].String())
//
//		// send second signal - should go to second receiver
//		tasks, _ = bufferedScheduler.Send("distributed", lua.LString("second"))
//		assert.Equal(t, 1, len(tasks), "second receiver should be resumed")
//
//		tasks, err = bufferedScheduler.Step(vm, tasks...)
//		assert.NoError(t, err)
//		assert.Equal(t, "second_done", tasks[0].Yielded[0].String())
//
//		// Channel should no longer be listened
//		assert.Equal(t, 0, len(bufferedScheduler.GetOpenChannels()))
//	})
//}
//
//func TestExternalChannelSelect(t *testing.T) {
//	logger := zap.NewNop()
//
//	t.Run("select on inbox channel", func(t *testing.T) {
//		bufferedScheduler := NewRuntime()
//		channels := NewChannelModule()
//
//		vm, err := engine.NewCoroutineVM(
//			context.Background(), logger,
//			engine.WithPreloaded(channels.Name(), channels.Loader),
//		)
//		assert.NoError(t, err)
//		defer vm.Close()
//
//		err = vm.PushScript(`
//			local ext = channel.inbox("ext1")
//
//			coroutine.go(function()
//				local result = channel.select({
//					ext:case_receive()
//				})
//
//				assert(result.channel == ext, "wrong channel selected")
//				assert(result.value == "test_data", "wrong value received")
//				assert(result.ok, "receive should succeed")
//				coroutine.yield("receive_complete")
//			end)
//		`, "test")
//		assert.NoError(t, err)
//
//		// Get initial task - this registers the receiver
//		tasks, err := bufferedScheduler.Step(vm)
//		assert.NoError(t, err)
//
//		// Verify channel is registered
//		listeners := bufferedScheduler.GetOpenChannels()
//		assert.Equal(t, []string{"ext1"}, listeners, "channel should be registered")
//
//		// send data to channel
//		tasks, _ = bufferedScheduler.Send("ext1", lua.LString("test_data"))
//		assert.Equal(t, 1, len(tasks), "should have one task to resume")
//
//		// Process resumed task
//		tasks, err = bufferedScheduler.Step(vm, tasks...)
//		assert.NoError(t, err)
//		assert.Equal(t, "receive_complete", tasks[0].Yielded[0].String())
//
//		// Channel should be unregistered
//		assert.Equal(t, 0, len(bufferedScheduler.GetOpenChannels()), "channel should be unregistered")
//	})
//
//	//t.Run("select between multiple inbox channels", func(t *testing.T) {
//	//	bufferedScheduler := NewRuntime()
//	//	channels := NewChannelModule()
//	//
//	//	vm, err := engine.NewCoroutineVM(
//	//		context.Background(), logger,
//	//		engine.WithPreloaded(channels.Name(), channels.Loader),
//	//	)
//	//	assert.NoError(t, err)
//	//	defer vm.Close()
//	//
//	//	err = vm.PushScript(`
//	//		local ext1 = channel.inbox("ext1")
//	//		local ext2 = channel.inbox("ext2")
//	//
//	//		coroutine.go(function()
//	//			-- First select should get ext1
//	//			local result = channel.select({
//	//				ext1:case_receive(),
//	//				ext2:case_receive()
//	//			})
//	//			assert(result.channel == ext1, "wrong channel selected")
//	//			assert(result.value == "data1", "wrong value received")
//	//			coroutine.yield("first_receive_complete")
//	//
//	//			-- Second select should get ext2
//	//			result = channel.select({
//	//				ext1:case_receive(),
//	//				ext2:case_receive()
//	//			})
//	//			assert(result.channel == ext2, "wrong channel selected")
//	//			assert(result.value == "data2", "wrong value received")
//	//			coroutine.yield("second_receive_complete")
//	//		end)
//	//	`, "test")
//	//	assert.NoError(t, err)
//	//
//	//	// Get initial task - this registers both receivers
//	//	tasks, err := bufferedScheduler.Step(vm)
//	//	assert.NoError(t, err)
//	//
//	//	// Verify both channels are registered
//	//	listeners := bufferedScheduler.GetOpenChannels()
//	//	assert.Equal(t, 2, len(listeners), "both channels should be registered")
//	//	assert.Contains(t, listeners, "ext1")
//	//	assert.Contains(t, listeners, "ext2")
//	//
//	//	// send to first channel
//	//	tasks = bufferedScheduler.send("ext1", lua.LString("data1"))
//	//	assert.Equal(t, 1, len(tasks), "should have one task to resume")
//	//
//	//	tasks, err = bufferedScheduler.Step(vm, tasks...)
//	//	assert.NoError(t, err)
//	//	assert.Equal(t, "first_receive_complete", tasks[0].Yielded[0].String())
//	//
//	//	// Step to register second set of receivers
//	//	tasks, err = bufferedScheduler.Step(vm, tasks...)
//	//	assert.NoError(t, err)
//	//
//	//	// send to second channel
//	//	tasks = bufferedScheduler.send("ext2", lua.LString("data2"))
//	//	assert.Equal(t, 1, len(tasks), "should have one task to resume")
//	//
//	//	tasks, err = bufferedScheduler.Step(vm, tasks...)
//	//	assert.NoError(t, err)
//	//	assert.Equal(t, "second_receive_complete", tasks[0].Yielded[0].String())
//	//
//	//	// All channels should be unregistered at end
//	//	assert.Equal(t, 0, len(bufferedScheduler.GetOpenChannels()), "no channels should remain registered")
//	//})
//}
//
//// TODO: ENSURE WE DEQUEUE CHANNELS WHEN SELECT TRIGGERED!!!!!!!!!!!!!!!!
//// TODO: EXTERNAL SIGNAL DOES NOT CLEAR UP CHANNEL PENDINGS!!!!!
//// TODO: WE HAVE TO DRAIN ALL THE SELECTS WHEN HAPPENS
//// TODO: WE HAVE FIND A WAY TO DE_REGISTER SIGNAL WHEN SELECT UNLOCKS IMMEDIATELY
//
//// todo: this is temp, TODO: DELETE IT!
//func (m *Module) newExternal(L *lua.LState) int {
//	ch := Named(L.CheckString(1), 0)
//	ud := L.NewUserData()
//	ud.Value = ch
//
//	L.SetMetatable(ud, L.GetTypeMetatable("channel"))
//	L.Push(ud)
//
//	return 1
//}
