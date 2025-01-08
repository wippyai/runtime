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

		scheduler := NewScheduler()
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

		scheduler := NewScheduler()
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

		scheduler := NewScheduler()
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

		scheduler := NewScheduler()
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

		scheduler := NewScheduler()
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

		scheduler := NewScheduler()
		tasks, err := scheduler.Step(vm)

		assert.NoError(t, err)

		tasks, err = scheduler.Step(vm, tasks...)
		assert.Error(t, err) // send to closed channel
	})
}
