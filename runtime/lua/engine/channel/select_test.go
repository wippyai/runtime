package channel

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestModule_ImmediateSelects(t *testing.T) {
	logger := zap.NewNop()

	t.Run("basic select operations", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Create channels
			local ch1 = channel.new(0)  -- unbuffered
			local ch2 = channel.new(1)  -- buffered with capacity 1
	
			-- Test 1: Select with send on buffered channel
			coroutine.spawn(function()
				-- Since ch2 is buffered with capacity 1, this should succeed immediately
				local result = channel.select({
					ch2:case_send("test1")
				})
	
				assert(result.channel == ch2, "wrong channel selected")
				assert(result.ok, "send should succeed")
				coroutine.yield("send_complete")
			end)
	
			-- Test 2: Select with receive on buffered channel
			coroutine.spawn(function()
				local result = channel.select({
					ch2:case_receive()
				})
				assert(result.channel == ch2, "wrong channel selected")
				assert(result.value == "test1", "wrong value received")
				assert(result.ok, "receive should succeed")
				coroutine.yield("receive_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		scheduler := NewRuntime()
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		var yields []string
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// Verify the sequence of operations
		expectedYields := []string{
			"send_complete",
			"receive_complete",
		}

		// Check that all expected yields occurred
		for _, expected := range expectedYields {
			assert.Contains(t, yields, expected, "missing yield: %s", expected)
		}
	})

	t.Run("select with default", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Create channels
			local ch1 = channel.new(0)  -- unbuffered
			local ch2 = channel.new(1)  -- buffered with capacity 1
	
			-- Test default case when no operations are ready (table style)
			local result = channel.select{
				ch1:case_receive(),
				ch2:case_receive(),
				default = true
			}
	
			assert(result.channel == nil, "first: default case should be selected")
	   	assert(result.ok == true, "first: default case should indicate success")
	   	assert(result.value == nil, "first: default case should have nil value")
	
			-- Test default case with parameter style
			result = channel.select({
				ch1:case_receive(),
				ch2:case_receive(),
			}, true)
	
			assert(result.channel == nil, "default case should be selected")
			assert(result.ok == true, "default case should indicate success")
			assert(result.value == nil, "first: default case should have nil value")
	
			-- Test that default is not selected when operation is ready
			ch2:send("test")
	
			result = channel.select({
				ch1:case_receive(),
				ch2:case_receive(),
			}, true)
	
			assert(result.channel == ch2, "ready channel should be selected over default")
			assert(result.value == "test", "wrong value received")
			assert(result.ok == true, "receive should succeed")
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("select on same channel", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
	   -- Test buffered channel (capacity 1)
	   local ch = channel.new(1)
	
	   -- Test 1: Empty buffer, send should succeed
	   local result = channel.select{
	       ch:case_send("test"),
	       ch:case_receive(),
	       default = true
	   }
	   assert(result.channel == ch, "send should succeed on empty buffered channel")
	   assert(result.ok == true, "send should succeed")
	
	   -- Test 2: Now buffer has value, receive should succeed
	   result = channel.select{
	       ch:case_send("test2"),
	       ch:case_receive(),
	       default = true
	   }
	   assert(result.channel == ch, "receive should succeed on non-empty buffered channel")
	   assert(result.value == "test", "should receive first sent value")
	   assert(result.ok == true, "receive should succeed")
	
	   -- Test unbuffered channel (should fall to default)
	   local unbuffered = channel.new(0)
	   result = channel.select{
	       unbuffered:case_send("test"),
	       unbuffered:case_receive(),
	       default = true
	   }
	   assert(result.channel == nil, "unbuffered channel should fall to default")
	   assert(result.ok == true, "default case should indicate success")
	`, "test")

		assert.NoError(t, err)
	})

	t.Run("select with closed channels", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
		-- Test receive on closed channel
		local ch = channel.new(1)
		ch:close()
		coroutine.yield("closed")

		-- Rest can be immediate operations
		local result = channel.select{
			ch:case_receive(),
			default = true
		}
		assert(result.channel == ch, "closed channel receive should be selected")
		assert(result.ok == false, "receive on closed should set ok=false")
		assert(result.value == nil, "receive on closed should return nil")

		-- Test send to closed channel (should error)
		local ok, err = pcall(function()
			channel.select{
				ch:case_send("test")
			}
		end)
		assert(not ok, "send on closed channel should error")
		assert(string.find(err, "attempt to send on closed channel"), 
			   "wrong error message: " .. err)

		-- Test receive when there's a value before close
		local ch2 = channel.new(1)
		ch2:send("last value")
		
		ch2:close()
		coroutine.yield("closed2")

		result = channel.select{
			ch2:case_receive()
		}
		assert(result.channel == ch2, "should receive from closed channel with value")
		assert(result.value == "last value", "should get buffered value")
		assert(result.ok == true, "receive buffered value should set ok=true")
		
		-- Next receive should indicate closed
		result = channel.select{
			ch2:case_receive()
		}
		assert(result.channel == ch2, "should receive from empty closed channel")
		assert(result.value == nil, "should get nil from empty closed channel")
		assert(result.ok == false, "should indicate channel closed")
    `, "test")

		assert.NoError(t, err)

		scheduler := NewRuntime()
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		var yields []string
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}

			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// Verify close operations completed
		assert.Contains(t, yields, "closed", "channel not closed")
		assert.Contains(t, yields, "closed2", "second channel not closed")
	})
}

func TestModule_BlockingSelects(t *testing.T) {
	logger := zap.NewNop()

	t.Run("basic blocking select", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Create channels
			local ch1 = channel.new(0)  -- unbuffered
			local ch2 = channel.new(0)  -- unbuffered
	
			-- First coroutine: blocks on select waiting for send or receive
			coroutine.spawn(function()
				local result = channel.select({
					ch1:case_receive(),
					ch2:case_send("test")
				})
	
				-- Should only get here after one operation succeeds
				assert(result.channel == ch2, "wrong channel selected")
				assert(result.ok, "operation should succeed")
				coroutine.yield("select_complete")
			end)
	
			-- Second coroutine: sleeps then enables one of the operations
			coroutine.spawn(function()
				coroutine.yield("helper_starting")
				-- Receive from ch2, allowing the blocked send to complete
				local msg, ok = ch2:receive()
				assert(ok and msg == "test", "wrong message received")
				coroutine.yield("helper_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		scheduler := NewRuntime()
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		var yields []string
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// Verify the sequence of operations
		expectedYields := []string{
			"helper_starting",
			"helper_complete",
			"select_complete",
		}

		assert.Equal(t, expectedYields, yields, "incorrect yield sequence")
	})

	t.Run("basic blocking select inverted", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Create channels
			local ch1 = channel.new(0)  -- unbuffered
			local ch2 = channel.new(0)  -- unbuffered
	
			-- First coroutine: blocks on select waiting for send or receive
			coroutine.spawn(function()
				local result = channel.select({
					ch1:case_receive(),
					ch2:case_send("test")
				})
	
				-- Should only get here after one operation succeeds
				assert(result.channel == ch1, "wrong channel selected")
				assert(result.ok, "operation should succeed")
				assert(result.value == "test_value", "wrong value received")
	
				coroutine.yield("select_complete")
			end)
	
			-- Second coroutine: sleeps then enables one of the operations
			coroutine.spawn(function()
				coroutine.yield("helper_starting")
				local ok = ch1:send("test_value")
				assert(ok, "can not send")
				coroutine.yield("helper_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		scheduler := NewRuntime()
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		var yields []string
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// Verify the sequence of operations
		expectedYields := []string{
			"helper_starting",
			"helper_complete",
			"select_complete",
		}

		assert.Equal(t, expectedYields, yields, "incorrect yield sequence")
	})

	t.Run("basic blocking select closed on receive", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Create channels
			local ch1 = channel.new(0)  -- unbuffered
			local ch2 = channel.new(0)  -- unbuffered
	
			-- First coroutine: blocks on select waiting for send or receive
			coroutine.spawn(function()
				local result = channel.select({
					ch1:case_receive(),
					ch2:case_send("test")
				})
	
				-- Should only get here after one operation succeeds
				assert(result.channel == ch1, "must indicate closed channel")
				assert(result.ok == false, "operation should indicate closed channel")
				coroutine.yield("select_complete")
			end)
	
			-- Second coroutine: sleeps then enables one of the operations
			coroutine.spawn(function()
				coroutine.yield("helper_starting")
				local ok = ch1:close()
				assert(ok, "can not close")
				coroutine.yield("helper_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		scheduler := NewRuntime()
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		var yields []string
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// Verify the sequence of operations
		expectedYields := []string{
			"helper_starting",
			"helper_complete",
			"select_complete",
		}

		assert.Equal(t, expectedYields, yields, "incorrect yield sequence")
	})

	t.Run("basic blocking select closed on send", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Create channels
			local ch1 = channel.new(0)  -- unbuffered
			local ch2 = channel.new(0)  -- unbuffered
			
			-- First coroutine: blocks on select waiting for send or receive
			coroutine.spawn(function()	
				-- will panic!
				channel.select{
					ch1:case_receive(),
					ch2:case_send("test")
				}
			end)
			
			-- Second coroutine: sleeps then closes ch2
			coroutine.spawn(function()
				coroutine.yield("helper_starting")
				local ok = ch2:close() -- Close ch2 (the send channel)
				assert(ok, "cannot close ch2")
				coroutine.yield("helper_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		scheduler := NewRuntime()
		tasks, err := scheduler.Step(vm)

		assert.NoError(t, err)

		tasks, err = scheduler.Step(vm, tasks...)
		assert.Error(t, err) // send to closed channel
	})
}

func TestModule_AdditionalSelectScenarios(t *testing.T) {
	logger := zap.NewNop()

	t.Run("select with multiple ready operations", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Create buffered channels and populate them
			local ch1 = channel.new(1)
			local ch2 = channel.new(1)
			
			ch1:send("value1")
			ch2:send("value2")
			
			-- Both channels have values, select should pick one
			local result = channel.select({
				ch1:case_receive(),
				ch2:case_receive()
			})
			
			-- Verify we got one of the values
			assert(result.channel == ch1 or result.channel == ch2, "should select one of the ready channels")
			assert(result.value == "value1" or result.value == "value2", "should receive one of the values")
			assert(result.ok == true, "receive should succeed")
			
			-- The other channel should still have its value
			local remaining = result.channel == ch1 and ch2 or ch1
			local val, ok = remaining:receive()
			assert(ok and (val == "value1" or val == "value2"), "other channel should still have its value")
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("select with error cases", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Test invalid select cases
			local ch = channel.new(0)
			
			-- Test empty select without default
			local ok, err = pcall(function()
				channel.select({})
			end)
			assert(not ok, "empty select without default should error")
			assert(string.find(err, "select with no cases and no default"), 
				   "wrong error message: " .. err)
			
			-- Test invalid case type
			ok, err = pcall(function()
				channel.select({
					"not a case"
				})
			end)
			assert(not ok, "invalid case should error")
			assert(string.find(err, "invalid select case"), 
				   "wrong error message: " .. err)
			
			-- Test duplicate default cases
			ok, err = pcall(function()
				channel.select({
					default = true,
					ch:case_receive(),
					default = true
				})
			end)
			assert(not ok, "duplicate default should error")
			assert(string.find(err, "multiple default cases"), 
				   "wrong error message: " .. err)
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("select with mixed send/receive on same channel", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(0)  -- unbuffered
			
			-- First coroutine: select with both send and receive on same channel
			coroutine.spawn(function()
				local result = channel.select({
					ch:case_send("test"),
					ch:case_receive()
				})
				
				-- One operation should complete once helper enables it
				assert(result.channel == ch, "wrong channel selected")
				assert(result.ok == true, "operation should succeed")
				coroutine.yield("select_complete")
			end)
			
			-- Helper coroutine enables one of the operations
			coroutine.spawn(function()
				coroutine.yield("helper_starting")
				local msg = ch:send("helper_value")
				assert(msg, "send should succeed")
				coroutine.yield("helper_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		scheduler := NewRuntime()
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		var yields []string
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		expectedYields := []string{
			"helper_starting",
			"helper_complete",
			"select_complete",
		}
		assert.Equal(t, expectedYields, yields, "incorrect yield sequence")
	})

	t.Run("select with multiple coroutines blocking", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(0)  -- unbuffered
			
			-- Multiple coroutines blocking on select
			for i = 1, 3 do
				coroutine.spawn(function()
					local result = channel.select({
						ch:case_receive()
					})
					assert(result.channel == ch, "wrong channel selected")
					assert(result.ok == true, "receive should succeed")
					assert(result.value == "value" .. i, "wrong value received")
					coroutine.yield("receiver_" .. i .. "_complete")
				end)
			end
			
			-- Helper sends values to unblock them one by one
			coroutine.spawn(function()
				coroutine.yield("sender_starting")
				for i = 1, 3 do
					local ok = ch:send("value" .. i)
					assert(ok, "send should succeed")
					coroutine.yield("sent_" .. i)
				end
			end)
		`, "test")

		assert.NoError(t, err)

		scheduler := NewRuntime()
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		var yields []string
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// Verify all receivers got values and sender completed
		assert.Contains(t, yields, "sender_starting", "sender didn't start")
		assert.Contains(t, yields, "sent_1", "first send didn't complete")
		assert.Contains(t, yields, "sent_2", "second send didn't complete")
		assert.Contains(t, yields, "sent_3", "third send didn't complete")
		assert.Contains(t, yields, "receiver_1_complete", "first receiver didn't complete")
		assert.Contains(t, yields, "receiver_2_complete", "second receiver didn't complete")
		assert.Contains(t, yields, "receiver_3_complete", "third receiver didn't complete")
	})

	//t.Run("select cleanup after trigger", func(t *testing.T) {
	//	vm, err := engine.NewCoroutineVM(
	//		context.Background(), logger,
	//		engine.WithPreloaded("channel", NewChannelModule().Loader),
	//	)
	//	assert.NoError(t, err)
	//	defer vm.Close()
	//
	//	err = vm.PushScript(`
	//    print("DEBUG: Test starting")
	//    -- Create unbuffered channels
	//    local ch1 = channel.new(0)
	//    local ch2 = channel.new(0)
	//    local ch3 = channel.new(0)
	//
	//    print("DEBUG: Spawning select coroutine")
	//    -- First coroutine blocks on select with multiple receives
	//    coroutine.spawn(function()
	//        print("DEBUG: Inside select coroutine")
	//        local result = channel.select({
	//            ch1:case_receive(),
	//            ch2:case_receive(),
	//            ch3:case_receive()
	//        })
	//        -- Should select ch1 since that's the one we send to
	//        print("DEBUG: Select completed, selected channel:", tostring(result.channel))
	//        assert(result.channel == ch1, "wrong channel selected - expected ch1")
	//        assert(result.value == "test_value", "wrong value received")
	//        coroutine.yield("select_complete")
	//    end)
	//
	//    print("DEBUG: Spawning helper coroutine")
	//    -- Helper coroutine sends to trigger select
	//    coroutine.spawn(function()
	//        coroutine.yield("helper_starting")
	//
	//        print("DEBUG: Helper sending to ch1")
	//        -- send to ch1 specifically
	//        local ok = ch1:send("test_value")
	//        assert(ok, "send to ch1 failed")
	//        coroutine.yield("helper_complete")
	//
	//        print("DEBUG: Helper verifying cleanup")
	//        -- Now verify ch2 and ch3 were cleaned up by doing sends
	//        -- These should work immediately if cleanup was successful
	//        ok = ch2:send("verify2")
	//        assert(ok, "ch2 send failed - not cleaned up")
	//        local v2 = ch2:receive()
	//        assert(v2 == "verify2", "ch2 receive failed")
	//        coroutine.yield("ch2_verified")
	//
	//        ok = ch3:send("verify3")
	//        assert(ok, "ch3 send failed - not cleaned up")
	//        local v3 = ch3:receive()
	//        assert(v3 == "verify3", "ch3 receive failed")
	//        coroutine.yield("ch3_verified")
	//    end)
	//`, "test")
	//	assert.NoError(t, err)
	//
	//	scheduler := NewRuntime()
	//	fmt.Printf("\nDEBUG: Starting scheduler steps\n")
	//
	//	tasks, err := scheduler.Step(vm)
	//	assert.NoError(t, err)
	//
	//	var yields []string
	//	for len(tasks) > 0 {
	//		for _, task := range tasks {
	//			if vals := task.Yielded; len(vals) > 0 {
	//				fmt.Printf("DEBUG: Got yield: %s\n", vals[0].String())
	//				yields = append(yields, vals[0].String())
	//			}
	//		}
	//		tasks, err = scheduler.Step(vm, tasks...)
	//		assert.NoError(t, err)
	//	}
	//
	//	// Verify sequence of operations
	//	expectedYields := []string{
	//		"helper_starting",
	//		"helper_complete",
	//		"select_complete",
	//		"ch2_verified",
	//		"ch3_verified",
	//	}
	//	assert.Equal(t, expectedYields, yields, "incorrect yield sequence")
	//})
	// todo: uncomment
}

func TestExternalChannelSelect(t *testing.T) {
	logger := zap.NewNop()

	t.Run("select on inbox channel", func(t *testing.T) {
		scheduler := NewRuntime()
		channels := NewChannelModule()

		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded(channels.Name(), channels.Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ext = channel.inbox("ext1")
			
			coroutine.spawn(function()
				local result = channel.select({
					ext:case_receive()
				})

				assert(result.channel == ext, "wrong channel selected")
				assert(result.value == "test_data", "wrong value received")
				assert(result.ok, "receive should succeed")
				coroutine.yield("receive_complete")
			end)
		`, "test")
		assert.NoError(t, err)

		// Get initial task - this registers the receiver
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		// Verify channel is registered
		listeners := scheduler.GetActiveSignals()
		assert.Equal(t, []string{"ext1"}, listeners, "channel should be registered")

		// send data to channel
		tasks, _ = scheduler.Send("ext1", lua.LString("test_data"))
		assert.Equal(t, 1, len(tasks), "should have one task to resume")

		// Process resumed task
		tasks, err = scheduler.Step(vm, tasks...)
		assert.NoError(t, err)
		assert.Equal(t, "receive_complete", tasks[0].Yielded[0].String())

		// Channel should be unregistered
		assert.Equal(t, 0, len(scheduler.GetActiveSignals()), "channel should be unregistered")
	})

	//t.Run("select between multiple inbox channels", func(t *testing.T) {
	//	scheduler := NewRuntime()
	//	channels := NewChannelModule()
	//
	//	vm, err := engine.NewCoroutineVM(
	//		context.Background(), logger,
	//		engine.WithPreloaded(channels.Name(), channels.Loader),
	//	)
	//	assert.NoError(t, err)
	//	defer vm.Close()
	//
	//	err = vm.PushScript(`
	//		local ext1 = channel.inbox("ext1")
	//		local ext2 = channel.inbox("ext2")
	//
	//		coroutine.spawn(function()
	//			-- First select should get ext1
	//			local result = channel.select({
	//				ext1:case_receive(),
	//				ext2:case_receive()
	//			})
	//			assert(result.channel == ext1, "wrong channel selected")
	//			assert(result.value == "data1", "wrong value received")
	//			coroutine.yield("first_receive_complete")
	//
	//			-- Second select should get ext2
	//			result = channel.select({
	//				ext1:case_receive(),
	//				ext2:case_receive()
	//			})
	//			assert(result.channel == ext2, "wrong channel selected")
	//			assert(result.value == "data2", "wrong value received")
	//			coroutine.yield("second_receive_complete")
	//		end)
	//	`, "test")
	//	assert.NoError(t, err)
	//
	//	// Get initial task - this registers both receivers
	//	tasks, err := scheduler.Step(vm)
	//	assert.NoError(t, err)
	//
	//	// Verify both channels are registered
	//	listeners := scheduler.GetActiveSignals()
	//	assert.Equal(t, 2, len(listeners), "both channels should be registered")
	//	assert.Contains(t, listeners, "ext1")
	//	assert.Contains(t, listeners, "ext2")
	//
	//	// send to first channel
	//	tasks = scheduler.send("ext1", lua.LString("data1"))
	//	assert.Equal(t, 1, len(tasks), "should have one task to resume")
	//
	//	tasks, err = scheduler.Step(vm, tasks...)
	//	assert.NoError(t, err)
	//	assert.Equal(t, "first_receive_complete", tasks[0].Yielded[0].String())
	//
	//	// Step to register second set of receivers
	//	tasks, err = scheduler.Step(vm, tasks...)
	//	assert.NoError(t, err)
	//
	//	// send to second channel
	//	tasks = scheduler.send("ext2", lua.LString("data2"))
	//	assert.Equal(t, 1, len(tasks), "should have one task to resume")
	//
	//	tasks, err = scheduler.Step(vm, tasks...)
	//	assert.NoError(t, err)
	//	assert.Equal(t, "second_receive_complete", tasks[0].Yielded[0].String())
	//
	//	// All channels should be unregistered at end
	//	assert.Equal(t, 0, len(scheduler.GetActiveSignals()), "no channels should remain registered")
	//})
}

// TODO: ENSURE WE DEQUEUE CHANNELS WHEN SELECT TRIGGERED!!!!!!!!!!!!!!!!
// TODO: EXTERNAL SIGNAL DOES NOT CLEAR UP CHANNEL PENDINGS!!!!!
// TODO: WE HAVE TO DRAIN ALL THE SELECTS WHEN HAPPENS
// TODO: WE HAVE FIND A WAY TO DE_REGISTER SIGNAL WHEN SELECT UNLOCKS IMMEDIATELY
