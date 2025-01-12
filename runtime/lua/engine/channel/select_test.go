package channel

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
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
			coroutine.go(function()
				-- Since ch2 is buffered with capacity 1, this should succeed immediately
				local result = channel.select({
					ch2:case_send("test1")
				})
	
				assert(result.channel == ch2, "wrong channel selected")
				assert(result.ok, "send should succeed")
				coroutine.yield("send_complete")
			end)
	
			-- Test 2: Select with receive on buffered channel
			coroutine.go(function()
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

	t.Run("immediate select with buffered channels", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Test 1: Immediate receive from buffered channel
			local ch1 = channel.new(1)
			ch1:send("value1")
			
			local result = channel.select({
				ch1:case_receive()
			})
			assert(result.channel == ch1, "wrong channel selected")
			assert(result.value == "value1", "wrong value received")
			assert(result.ok == true, "receive should succeed")
			
			-- Test 2: Immediate send to non-full buffered channel
			local ch2 = channel.new(1)
			result = channel.select({
				ch2:case_send("value2")
			})
			assert(result.channel == ch2, "wrong channel selected")
			assert(result.ok == true, "send should succeed")
			
			-- Test 3: Multiple ready operations
			local ch3 = channel.new(1)
			local ch4 = channel.new(1)
			ch3:send("value3")  -- Make receive ready
			                    -- ch4 is empty so send is ready
			
			result = channel.select({
				ch3:case_receive(),
				ch4:case_send("value4")
			})
			assert(result.channel == ch3 or result.channel == ch4, 
				   "should select one of the ready channels")
			if result.channel == ch3 then
				assert(result.value == "value3", "wrong value received")
			else
				assert(result.ok == true, "send should succeed")
			end
			
			-- Test 4: Mix of ready and blocking operations
			local ch5 = channel.new(1)
			local ch6 = channel.new(0)  -- unbuffered, will block
			ch5:send("value5")
			
			result = channel.select({
				ch5:case_receive(),  -- ready
				ch6:case_receive()   -- would block
			})
			assert(result.channel == ch5, "should select ready channel")
			assert(result.value == "value5", "wrong value received")
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
			coroutine.go(function()
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
			coroutine.go(function()
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
			coroutine.go(function()
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
			coroutine.go(function()
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
			coroutine.go(function()
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
			coroutine.go(function()
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
			coroutine.go(function()	
				-- will panic!
				channel.select{
					ch1:case_receive(),
					ch2:case_send("test")
				}
			end)
			
			-- Second coroutine: sleeps then closes ch2
			coroutine.go(function()
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
			coroutine.go(function()
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
			coroutine.go(function()
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
				coroutine.go(function()
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
			coroutine.go(function()
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
}

func TestModule_SelectQueueState(t *testing.T) {
	logger := zap.NewNop()
	t.Run("verify select queue state", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Create test channel
			local ch = channel.new(0)  -- unbuffered
			
			-- Spawn receiver that will block on select
			coroutine.go(function()
				local result = channel.select{
					ch:case_receive(),
					default = true
				}
				coroutine.yield("select_complete")
			end)
		`, "test")
		assert.NoError(t, err)

		scheduler := NewRuntime()

		// First step - should create coroutine and enqueue select
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		// Get scheduler and verify state after select is enqueued
		sch, ok := scheduler.scheduler.(*bufferedScheduler)
		if !ok {
			t.Fatal("scheduler is not bufferedScheduler")
		}

		assert.Empty(t, sch.senders.queues, "senders queue should be empty")
		assert.Equal(t, 0, len(sch.receivers.queues), "should have no receivers") // no select remains due to immediate completion

		// Complete the test
		var yields []string
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yield := vals[0].String()
					yields = append(yields, yield)
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// Verify final state - queues should be empty
		assert.Empty(t, sch.senders.queues, "senders queue should be empty at end")
		assert.Empty(t, sch.receivers.queues, "receivers queue should be empty at end")

		expectedYields := []string{
			"select_complete",
		}
		assert.Equal(t, expectedYields, yields, "unexpected yields")
	})
}

func TestModule_SelectQueueState_Blocking(t *testing.T) {
	logger := zap.NewNop()
	t.Run("verify select queue state", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`	
			-- Create test channel
			local ch = channel.new(0)  -- unbuffered
			
			-- Spawn receiver that will block on select
			coroutine.go(function()
				local result = channel.select{
					ch:case_receive(),
				}
				coroutine.yield("select_complete")
			end)
		`, "test")
		assert.NoError(t, err)

		scheduler := NewRuntime()

		_, err = scheduler.Step(vm)
		assert.NoError(t, err)

		// Get scheduler and verify state after select is enqueued
		sch, ok := scheduler.scheduler.(*bufferedScheduler)
		if !ok {
			t.Fatal("scheduler is not bufferedScheduler")
		}

		// Verify initial state - should have one receiver
		assert.Empty(t, sch.senders.queues, "senders queue should be empty")
		assert.Equal(t, 1, len(sch.receivers.queues), "should have one receiver queue")

		// state should be blocked
	})
}

func TestModule_SelectCleanup(t *testing.T) {
	logger := zap.NewNop()

	t.Run("select cleanup verification", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Create test channels
			local ch1 = channel.new(0)  -- unbuffered
			local ch2 = channel.new(0)  -- unbuffered
			local ch3 = channel.new(0)  -- unbuffered

			-- First coroutine: blocks on select with multiple receives
			coroutine.go(function()
				local result = channel.select{
					ch1:case_receive(),
					ch2:case_receive(),
					ch3:case_receive()
				}
				assert(result.channel == ch1, "wrong channel selected")
				assert(result.value == "test1", "wrong value received")
				coroutine.yield("select_complete")
			end)

			-- Helper coroutine: triggers select and verifies cleanup
			coroutine.go(function()
				coroutine.yield("helper_starting")
				
				-- Trigger select
				assert(ch1:send("test1"))
				coroutine.yield("send_complete")

				-- Verify cleanup: try non-blocking send on ch2 and ch3
				-- If cleanup worked, these should fail (fall to default) since channels are unbuffered
				-- and should have no lingering receivers
				local result = channel.select{
					ch2:case_send("verify2"),
					default = true
				}
				assert(result.channel == nil, "ch2 should have no receivers")
				coroutine.yield("ch2_verified")

				result = channel.select{
					ch3:case_send("verify3"),
					default = true
				}
				assert(result.channel == nil, "ch3 should have no receivers")
				coroutine.yield("ch3_verified")
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
					yield := vals[0].String()
					yields = append(yields, yield)
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			if err != nil {
				t.Fatalf("Scheduler error: %v", err)
			}
		}

		sch, ok := scheduler.scheduler.(*bufferedScheduler)
		if !ok {
			t.Fatalf("scheduler is not bufferedScheduler")
			return
		}

		// Verify cleanup
		assert.Empty(t, sch.senders.queues, "senders queue should be empty")
		assert.Empty(t, sch.receivers.queues, "receivers queue should be empty")

		expectedYields := []string{
			"helper_starting",
			"send_complete",
			"select_complete",
			"ch2_verified",
			"ch3_verified",
		}
		assert.Equal(t, expectedYields, yields, "operations did not complete in expected order")
	})
}

func TestModule_SelectCleanupOnClose(t *testing.T) {
	logger := zap.NewNop()

	t.Run("verify select cleanup on channel close", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
            -- Create test channels
            local ch1 = channel.new(0)  -- unbuffered
            local ch2 = channel.new(0)  -- unbuffered

            -- First coroutine: blocks on select with multiple operations
            coroutine.go(function()
                local result = channel.select{
                    ch1:case_receive(),
                    ch2:case_receive()
                }
                assert(result.channel == ch1, "wrong channel selected")
                assert(result.ok == false, "should indicate closed channel")
                assert(result.value == nil, "should receive nil from closed channel")
                coroutine.yield("select_complete")
            end)

            -- Second coroutine: different select operation on same channels
            coroutine.go(function()
                local result = channel.select{
                    ch1:case_receive(),
                    ch2:case_receive()
                }
                assert(result.channel == ch1, "wrong channel selected")
                assert(result.ok == false, "should indicate closed channel")
                assert(result.value == nil, "should receive nil from closed channel")
                coroutine.yield("select2_complete")
            end)

            -- Helper coroutine: closes ch1 to trigger select completion
            coroutine.go(function()
                coroutine.yield("helper_starting")
                
                -- Close ch1 which should trigger both selects
                assert(ch1:close())
                coroutine.yield("close_complete")

                -- Verify cleanup: try non-blocking operations on ch2
                local result = channel.select{
                    ch2:case_receive(),
                    default = true
                }
                assert(result.channel == nil, "ch2 should have no pending operations")
                coroutine.yield("ch2_verified")

                -- Double check queues are empty by trying a send
                result = channel.select{
                    ch2:case_send("verify"),
                    default = true
                }
                assert(result.channel == nil, "ch2 should have no pending operations")
                coroutine.yield("ch2_verified_send")
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
					yield := vals[0].String()
					yields = append(yields, yield)
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			if err != nil {
				t.Fatalf("Scheduler error: %v", err)
			}
		}

		sch, ok := scheduler.scheduler.(*bufferedScheduler)
		if !ok {
			t.Fatal("scheduler is not bufferedScheduler")
			return
		}

		// Verify final state - all queues should be empty
		assert.Empty(t, sch.senders.queues, "senders queue should be empty")
		assert.Empty(t, sch.receivers.queues, "receivers queue should be empty")

		expectedYields := []string{
			"helper_starting",
			"close_complete",
			"select_complete",
			"select2_complete",
			"ch2_verified",
			"ch2_verified_send",
		}
		assert.Equal(t, expectedYields, yields, "operations did not complete in expected order")
	})
}
