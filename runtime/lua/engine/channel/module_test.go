package channel

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"strings"
	"testing"
)

func TestChannels_Basic(t *testing.T) {
	logger := zap.NewNop()

	t.Run("channel comparison", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
        -- Create some channels
        local ch1 = channel.new(0)
        local ch2 = channel.new(0)
        local ch3 = ch1  -- Same reference as ch1
        
        -- Test equality
        assert(ch1 == ch3, "same channel should be equal")
        assert(ch1 ~= ch2, "different channels should not be equal")
        
        -- Test in table as key
        local channels = {}
        channels[ch1] = "channel1"
        channels[ch2] = "channel2"
        
        assert(channels[ch1] == "channel1", "channel table key lookup failed")
        assert(channels[ch3] == "channel1", "channel reference equality failed")
        assert(channels[ch2] == "channel2", "different channel lookup failed")
	`, "test")

		assert.NoError(t, err)

		tasks := vm.GetYieldedTasks()
		for len(tasks) > 0 {
			var err error
			tasks, err = vm.Step(tasks...)
			assert.NoError(t, err)
		}
	})

	t.Run("unbuffered channel send/receive", func(t *testing.T) {
		scheduler := NewRuntime()
		channels := NewChannelModule()

		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded(channels.Name(), channels.Loader),
		)

		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(0)  -- unbuffered channel
			
			-- Sender
			coroutine.spawn(function()
				ch:send("hello")
				coroutine.yield("send_complete")
			end)
			
			-- Receiver
			coroutine.spawn(function()
				local msg, ok = ch:receive()
				assert(ok, "expected successful receive")
				assert(msg == "hello", "wrong message received")
				coroutine.yield("receive_complete")
			end)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		// Get initial yielded tasks
		tasks, _ := scheduler.Step(vm)
		assert.Equal(t, 2, len(tasks), "expected 2 yielded tasks")

		// Step all tasks until completion
		for len(tasks) > 0 {
			var err error
			tasks, err = scheduler.Step(vm, tasks...)
			if err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestModule_Operations(t *testing.T) {
	logger := zap.NewNop()

	t.Run("buffered channel operations", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(2)  -- buffered channel capacity 2

			-- Sender goroutine
			coroutine.spawn(function()
				assert(ch:send("first"), "first send should succeed")
				assert(ch:send("second"), "second send should succeed")
				local ok = ch:send("third")  -- should block
				assert(not ok, "third send should fail on full buffer")
				coroutine.yield("sender_blocked")
			end)

			-- Receiver goroutine
			coroutine.spawn(function()
				local msg1, ok1 = ch:receive()
				assert(ok1 and msg1 == "first", "first receive failed")
				
				local msg2, ok2 = ch:receive()
				assert(ok2 and msg2 == "second", "second receive failed")
				
				coroutine.yield("receiver_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		tasks := vm.GetYieldedTasks()
		for len(tasks) > 0 {
			var err error
			tasks, err = vm.Step(tasks...)
			assert.NoError(t, err)
		}
	})

	t.Run("channel close behavior", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(1)

			-- Sender with close
			coroutine.spawn(function()
				assert(ch:send("buffered"), "send should succeed")
				ch:close()
				
				-- Verify can't send on closed channel
				local ok, err = pcall(function() 
					ch:send("after close")
				end)
				assert(not ok and string.find(err, "closed channel"), 
					"send on closed channel should error")
				
				coroutine.yield("sender_done")
			end)

			-- Multiple receivers
			for i = 1, 2 do
				coroutine.spawn(function()
					local msg, ok = ch:receive()
					if i == 1 then
						assert(ok and msg == "buffered", 
							"should receive buffered message")
					else
						assert(not ok and msg == nil, 
							"should get nil on closed empty channel")
					end
					coroutine.yield("receiver" .. i .. "_done")
				end)
			end
		`, "test")

		assert.NoError(t, err)

		tasks := vm.GetYieldedTasks()
		for len(tasks) > 0 {
			var err error
			tasks, err = vm.Step(tasks...)
			assert.NoError(t, err)
		}
	})

	t.Run("multiple senders and receivers", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(1) -- Buffer size 1
			local received = {}
			local sent = {}

			-- Multiple senders
			for i = 1, 3 do
				coroutine.spawn(function()
					local msg = "msg" .. i
					local ok = ch:send(msg)
					sent[i] = ok and msg or "failed"
					coroutine.yield("sender" .. i .. "_done")
				end)
			end

			-- Multiple receivers
			for i = 1, 3 do
				coroutine.spawn(function()
					local msg, ok = ch:receive()
					received[i] = ok and msg or "failed"
					coroutine.yield("receiver" .. i .. "_done")
				end)
			end
			
			-- Wait for completion and verify
			coroutine.spawn(function()
				-- Verify we got all messages
				assert(#received == 3, "should receive 3 messages")
				assert(#sent == 3, "should send 3 messages")
				
				-- Verify messages were received in order
				local msgs = {}
				for _, msg in ipairs(received) do
					if msg ~= "failed" then
						table.insert(msgs, msg)
					end
				end
				table.sort(msgs)
				assert(msgs[1] == "msg1" and msgs[2] == "msg2" and msgs[3] == "msg3",
					"messages not received correctly")
					
				coroutine.yield("verify_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		tasks := vm.GetYieldedTasks()
		for len(tasks) > 0 {
			var err error
			tasks, err = vm.Step(tasks...)
			assert.NoError(t, err)
		}
	})

	t.Run("zero capacity channel", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(0)
			
			-- Test synchronous send/receive
			coroutine.spawn(function()
				ch:send("sync")
				coroutine.yield("send_complete")
			end)
			
			coroutine.spawn(function()
				local msg, ok = ch:receive()
				assert(ok and msg == "sync", "sync receive failed")
				coroutine.yield("receive_complete")
			end)
			
			-- Verify can't buffer
			coroutine.spawn(function()
				local ok = ch:send("should block")
				assert(not ok, "send should block on unbuffered channel")
				coroutine.yield("blocked_send")
			end)
		`, "test")

		assert.NoError(t, err)

		tasks := vm.GetYieldedTasks()
		for len(tasks) > 0 {
			var err error
			tasks, err = vm.Step(tasks...)
			assert.NoError(t, err)
		}
	})

	t.Run("invalid operations", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Test negative capacity
			local ok, err = pcall(function()
				channel.new(-1)
			end)
			assert(not ok and string.find(err, "capacity"), 
				"negative capacity should error")
			
			local ch = channel.new(0)
			
			-- Test close operations in coroutine
			coroutine.spawn(function()
				ch:close()
				
				-- Test double close
				ok, err = pcall(function()
					ch:close()
				end)
				assert(not ok and string.find(err, "closed"), 
					"double close should error")
				
				-- Test send after close
				ok, err = pcall(function()
					ch:send("test")
				end)
				assert(not ok and string.find(err, "closed"), 
					"send after close should error")
				
				coroutine.yield("invalid_ops_complete")
			end)
		`, "test")

		assert.NoError(t, err)
	})
}

func TestModule_YieldSequences(t *testing.T) {
	logger := zap.NewNop()

	t.Run("buffer wrapping with yield verification", func(t *testing.T) {
		scheduler := NewRuntime()
		channels := NewChannelModule()

		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded(channels.Name(), channels.Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
	       local ch = channel.new(3)
	       local sends_complete = false
	
	       -- Sender that fills buffer
	       coroutine.spawn(function()
	           -- Fill buffer
	           assert(ch:send("1"), "first send")
	           coroutine.yield("sent_1")
	
	           assert(ch:send("2"), "second send")
	           coroutine.yield("sent_2")
	
	           assert(ch:send("3"), "third send")
	           coroutine.yield("sent_3")
	
	           sends_complete = true
	           coroutine.yield("sends_complete")
	       end)
	
	       -- Receiver that waits for sends to complete
	       coroutine.spawn(function()
	           -- Wait for sends to complete
	           while not sends_complete do
	               coroutine.yield("waiting")
	           end
	
	           -- Now receive all values
	           local v1, ok = ch:receive()
	           assert(ok and v1 == "1", "receive 1")
	           coroutine.yield("received_1")
	
	           local v2, ok = ch:receive()
	           assert(ok and v2 == "2", "receive 2")
	           coroutine.yield("received_2")
	
	           local v3, ok = ch:receive()
	           assert(ok and v3 == "3", "receive 3")
	           coroutine.yield("received_3")
	
	           coroutine.yield("receiver_done")
	       end)
	   `, "test")

		assert.NoError(t, err)

		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		yields := []string{}
		for len(tasks) > 0 {
			for _, task := range tasks {
				if len(task.Yielded) > 0 {
					yields = append(yields, task.Yielded[0].String())
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// We care about the relative ordering of sends and receives,
		// not the exact sequence of "waiting" yields
		filteredYields := []string{}
		for _, y := range yields {
			if y != "waiting" {
				filteredYields = append(filteredYields, y)
			}
		}

		expectedYields := []string{
			"sent_1",
			"sent_2",
			"sent_3",
			"sends_complete",
			"received_1",
			"received_2",
			"received_3",
			"receiver_done",
		}

		assert.Equal(t, expectedYields, filteredYields, "incorrect yield sequence")
	})

	t.Run("value type preservation", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(1)
			local results = {}
	
			-- Test different Lua value types
			coroutine.spawn(function()
				-- String
				assert(ch:send("test string"))
				coroutine.yield("sent_string")
	
				-- Number
				assert(ch:send(42.5))
				coroutine.yield("sent_number")
	
				-- Boolean
				assert(ch:send(true))
				coroutine.yield("sent_boolean")
	
				-- Table
				local t = {key = "value", nested = {1, 2, 3}}
				assert(ch:send(t))
				coroutine.yield("sent_table")
	
				-- Nil
				assert(ch:send(nil))
				coroutine.yield("sent_nil")
	
				coroutine.yield("sends_complete")
			end)
	
			-- Receiver verifies type preservation
			coroutine.spawn(function()
				-- String
				local v, ok = ch:receive()
				assert(ok and type(v) == "string" and v == "test string")
				coroutine.yield("received_string")
	
				-- Number
				v, ok = ch:receive()
				assert(ok and type(v) == "number" and v == 42.5)
				coroutine.yield("received_number")
	
				-- Boolean
				v, ok = ch:receive()
				assert(ok and type(v) == "boolean" and v == true)
				coroutine.yield("received_boolean")
	
				-- Table
				v, ok = ch:receive()
				assert(ok and type(v) == "table")
				assert(v.key == "value" and v.nested[1] == 1)
				coroutine.yield("received_table")
	
				-- Nil
				v, ok = ch:receive()
				assert(ok and v == nil)
				coroutine.yield("received_nil")
	
				coroutine.yield("receives_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		var yields []string
		tasks, _ := vm.Step()
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}
			var err error
			tasks, err = vm.Step(tasks...)
			assert.NoError(t, err)
		}

		// Verify type preservation sequence
		expectedYields := []string{
			"sent_string",
			"received_string",
			"sent_number",
			"received_number",
			"sent_boolean",
			"received_boolean",
			"sent_table",
			"received_table",
			"sent_nil",
			"received_nil",
			"sends_complete",
			"receives_complete",
		}

		assert.Equal(t, expectedYields, yields, "incorrect type sequence")
	})

	t.Run("cleanup and reference handling", func(t *testing.T) {
		scheduler := NewRuntime()
		channels := NewChannelModule()

		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded(channels.Name(), channels.Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(3)
			local refs = {}
	
			-- Create some table references
			local t1 = {data = "table1"}
			local t2 = {data = "table2"}
	
			coroutine.spawn(function()
				-- Fill channel
				assert(ch:send(t1))
				coroutine.yield("sent_t1")
	
				assert(ch:send(t2))
				coroutine.yield("sent_t2")
	
				-- Close channel
				ch:close()
				coroutine.yield("channel_closed")
			end)
	
			-- Receiver verifies cleanup
			coroutine.spawn(function()
				-- Get values before cleanup
				local v1, ok = ch:receive()
				assert(ok and v1.data == "table1")
				coroutine.yield("received_t1")
	
				local v2, ok = ch:receive()
				assert(ok and v2.data == "table2")
				coroutine.yield("received_t2")
	
				-- Should get nil after close
				local v3, ok = ch:receive()
				assert(not ok and v3 == nil)
				coroutine.yield("received_nil")
			end)
		`, "test")

		assert.NoError(t, err)

		var yields []string
		tasks, _ := scheduler.Step(vm)
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}
			var err error
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		expectedYields := []string{
			"sent_t1",
			"received_t1",
			"sent_t2",
			"received_t2",
			"channel_closed",
			"received_nil",
		}

		assert.Equal(t, expectedYields, yields, "incorrect cleanup sequence")
	})

	t.Run("buffered channel with multiple coroutines", func(t *testing.T) {
		scheduler := NewRuntime()
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(2)  -- buffered channel capacity 2
			local received = {}
			local completed = {senders = 0, receivers = 0}

			-- Multiple senders
			for i = 1, 3 do
				coroutine.spawn(function()
					ch:send("msg" .. i)  -- Third send will block
					completed.senders = completed.senders + 1
					coroutine.yield("sender" .. i .. "_done")
				end)
			end

			-- Multiple receivers
			for i = 1, 3 do
				coroutine.spawn(function()
					local msg = ch:receive()  -- First two complete immediately
					received[i] = msg
					completed.receivers = completed.receivers + 1
					coroutine.yield("receiver" .. i .. "_done")
				end)
			end

			-- Closer
			coroutine.spawn(function()
				while completed.senders < 2 or completed.receivers < 2 do
					coroutine.yield("waiting")
				end
				ch:close()
				coroutine.yield("close_done")
			end)

			-- Verify final state
			coroutine.spawn(function()
				while completed.senders < 3 or completed.receivers < 3 do
					coroutine.yield("verify_waiting")
				end
				
				-- Check we got all messages
				assert(#received == 3, "should receive 3 messages")
				
				-- First two messages should be msg1 and msg2 in order
				assert(received[1] == "msg1", "first message wrong")
				assert(received[2] == "msg2", "second message wrong")
				assert(received[3] == "msg3", "third message wrong")
				
				coroutine.yield("verify_done")
			end)
		`, "test")

		assert.NoError(t, err)

		var yields []string
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					if str, ok := vals[0].(lua.LString); ok {
						yields = append(yields, string(str))
					}
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// Verify key operations completed in expected order
		assert.Contains(t, yields, "sender1_done")
		assert.Contains(t, yields, "sender2_done")
		assert.Contains(t, yields, "sender3_done")
		assert.Contains(t, yields, "receiver1_done")
		assert.Contains(t, yields, "receiver2_done")
		assert.Contains(t, yields, "receiver3_done")
		assert.Contains(t, yields, "close_done")
		assert.Contains(t, yields, "verify_done")

		// Verify ordering - first two sends/receives happen before close
		senderIdx := make(map[string]int)
		receiverIdx := make(map[string]int)
		var closeIdx int

		for i, y := range yields {
			switch y {
			case "sender1_done", "sender2_done":
				senderIdx[y] = i
			case "receiver1_done", "receiver2_done":
				receiverIdx[y] = i
			case "close_done":
				closeIdx = i
			}
		}

		// First two sends/receives should complete before close
		assert.Less(t, senderIdx["sender1_done"], closeIdx)
		assert.Less(t, senderIdx["sender2_done"], closeIdx)
		assert.Less(t, receiverIdx["receiver1_done"], closeIdx)
		assert.Less(t, receiverIdx["receiver2_done"], closeIdx)
	})
}

func TestMapReduce(t *testing.T) {
	logger := zap.NewNop()

	t.Run("parallel map reduce with 3 workers", func(t *testing.T) {
		scheduler := NewRuntime()
		channels := NewChannelModule()
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded(channels.Name(), channels.Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Create channels
			local workCh = channel.new(0)   -- Work distribution channel
			local resultCh = channel.new(0)  -- Results collection channel
			
			-- Input data: numbers 1 through 10
			local input = {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
			local expected_sum = 385  -- Sum of squares of input
			
			-- Map function: squares the number
			function map_fn(x)
				return x * x
			end
			
			-- Worker function that processes items from work channel
			function worker(id)
				while true do
					local num, ok = workCh:receive()
					if not ok then
						break  -- Channel closed
					end
					
					-- Process the number and send result
					local result = map_fn(num)
					resultCh:send({
						worker = id,
						input = num,
						result = result
					})
					coroutine.yield("worker_" .. id .. "_processed_" .. num)
				end
				coroutine.yield("worker_" .. id .. "_done")
			end
			
			-- Start 3 workers
			for i = 1, 3 do
				coroutine.spawn(function()
					worker(i)
				end)
			end
			
			-- Distributor coroutine that sends work
			coroutine.spawn(function()
				-- send all numbers to be processed
				for _, num in ipairs(input) do
					workCh:send(num)
					coroutine.yield("distributed_" .. num)
				end
				
				-- Close work channel to signal no more work
				workCh:close()
				coroutine.yield("distribution_complete")
			end)
			
			-- Reducer coroutine that collects and combines results
			coroutine.spawn(function()
				local results = {}
				local sum = 0
				
				-- Collect results until we have processed all input
				while #results < #input do
					local result = resultCh:receive()
					table.insert(results, result)
					sum = sum + result.result
					coroutine.yield("reduced_" .. result.input)
				end
				
				-- Verify results
				assert(#results == #input, "got wrong number of results")
				assert(sum == expected_sum, string.format(
					"wrong sum: expected %d, got %d", expected_sum, sum))
				
				-- Close result channel
				resultCh:close()
				coroutine.yield("reduce_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		// Process all coroutines until completion
		var yields []string
		tasks, err := scheduler.Step(vm)
		assert.NoError(t, err)

		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// Verify key stages completed
		assert.Contains(t, yields, "distribution_complete")
		assert.Contains(t, yields, "reduce_complete")

		// Verify all workers completed
		assert.Contains(t, yields, "worker_1_done")
		assert.Contains(t, yields, "worker_2_done")
		assert.Contains(t, yields, "worker_3_done")

		// Verify all numbers were processed
		for i := 1; i <= 10; i++ {
			found := false
			for _, y := range yields {
				if strings.Contains(y, fmt.Sprintf("reduced_%d", i)) {
					found = true
					break
				}
			}
			assert.True(t, found, "number %d was not processed", i)
		}
	})
}

func TestChannelPassing(t *testing.T) {
	logger := zap.NewNop()

	t.Run("passing channels through channels", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			-- Test a simple channel passing scenario first
			local ch1 = channel.new(1)  -- buffered channel
			local ch2 = channel.new(1)  -- channel to be passed
			
			-- First coroutine sends a channel
			coroutine.spawn(function()
				assert(ch2:send("test_message"))
				assert(ch1:send(ch2))
				coroutine.yield("sender_complete")
			end)
			
			-- Second coroutine receives the channel and reads from it
			coroutine.spawn(function()
				local receivedCh, ok = ch1:receive()
				assert(ok, "failed to receive channel")
				
				local msg, ok = receivedCh:receive()
				assert(ok and msg == "test_message", "wrong message: " .. tostring(msg))
				
				coroutine.yield("receiver_complete")
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

		// Verify basic channel passing works
		assert.Contains(t, yields, "sender_complete")
		assert.Contains(t, yields, "receiver_complete")

		// Now test a more complex scenario
		err = vm.PushScript(`
			-- Create channels for passing
			local controlCh = channel.new(1)  -- Control channel for passing worker channels
			local resultCh = channel.new(1)   -- Channel for results
			
			-- Worker that processes data
			coroutine.spawn(function()
				-- Create worker channel
				local workerCh = channel.new(1)
				
				-- Send my channel to manager
				assert(controlCh:send(workerCh))
				coroutine.yield("worker_sent_channel")
				
				-- Wait for work and process it
				local work, ok = workerCh:receive()
				assert(ok, "failed to receive work")
				
				-- Send result
				assert(resultCh:send(work * 2))
				coroutine.yield("worker_complete")
			end)
			
			-- Manager that coordinates work
			coroutine.spawn(function()
				-- Get worker channel
				local workerCh, ok = controlCh:receive()
				assert(ok, "failed to receive worker channel")
				coroutine.yield("manager_got_channel")
				
				-- Send work
				assert(workerCh:send(21))
				coroutine.yield("manager_sent_work")
				
				-- Get result
				local result, ok = resultCh:receive()
				assert(ok, "failed to receive result")
				assert(result == 42, "wrong result")
				
				coroutine.yield("manager_complete")
			end)
		`, "test")

		assert.NoError(t, err)

		tasks, err = scheduler.Step(vm)
		assert.NoError(t, err)

		yields = nil // Reset yields for second test
		for len(tasks) > 0 {
			for _, task := range tasks {
				if vals := task.Yielded; len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}
			}
			tasks, err = scheduler.Step(vm, tasks...)
			assert.NoError(t, err)
		}

		// Verify expected sequence
		expectedYields := []string{
			"worker_sent_channel",
			"manager_got_channel",
			"manager_sent_work",
			"worker_complete",
			"manager_complete",
		}

		for _, expected := range expectedYields {
			assert.Contains(t, yields, expected, "missing yield: %s", expected)
		}

		// Verify basic ordering constraints
		var workerSentIdx, managerGotIdx int
		for i, y := range yields {
			if y == "worker_sent_channel" {
				workerSentIdx = i
			}
			if y == "manager_got_channel" {
				managerGotIdx = i
			}
		}

		assert.Less(t, workerSentIdx, managerGotIdx,
			"worker should send channel before manager receives it")
	})
}

func TestChannelPassingWithCoreDebug(t *testing.T) {
	logger := zap.NewNop()

	t.Run("single worker with core debug", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
            -- Create channels
            local chanCh = channel.new(0)   -- Channel for passing worker channel
            local resultCh = channel.new(0)  -- Channel for results
            
            -- Worker
            coroutine.spawn(function()
                coroutine.yield("worker_start")
                
                -- Create worker channel
                local workerCh = channel.new(0)
                coroutine.yield("worker_created_channel")
                
                -- Send channel to manager
                chanCh:send(workerCh)
                coroutine.yield("worker_sent_channel")
                
                -- Wait for and process work
                local work = workerCh:receive()
                coroutine.yield("worker_got_work:" .. tostring(work))
                
                -- Send result
                resultCh:send(work * 2)
                coroutine.yield("worker_sent_result")
            end)
            
            -- Manager 
            coroutine.spawn(function()
                coroutine.yield("manager_start")
                
                -- Get worker channel
                local workerCh = chanCh:receive()
                coroutine.yield("manager_got_channel")
                
                -- Send work
                workerCh:send(42)
                coroutine.yield("manager_sent_work")
                
                -- Get result
                local result = resultCh:receive()
                coroutine.yield("manager_got_result:" .. tostring(result))
                
                assert(result == 84, "Wrong result")
                coroutine.yield("manager_verified")
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
				t.Logf("Error after yields:")
				for _, y := range yields {
					t.Logf("  %s", y)
				}
				t.Fatal(err)
			}
		}

		// Verify sequence
		expectedSequence := []string{
			"worker_start",
			"manager_start",
			"worker_created_channel",
			"worker_sent_channel",
			"manager_got_channel",
			"manager_sent_work",
			"worker_got_work:42",
			"worker_sent_result",
			"manager_got_result:84",
			"manager_verified",
		}

		assert.Equal(t, expectedSequence, yields, "Wrong operation sequence")
	})
}

func TestRapidBufferedOperations(t *testing.T) {
	logger := zap.NewNop()

	t.Run("buffer blocking behavior", func(t *testing.T) {
		vm, err := engine.NewCoroutineVM(
			context.Background(), logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		err = vm.PushScript(`
			local ch = channel.new(2)  -- smaller buffer for easier testing
			
			-- First coroutine fills buffer and blocks
			coroutine.spawn(function()
				assert(ch:send("value1"), "first send should succeed")
				assert(ch:send("value2"), "second send should succeed")
				-- This should block since buffer is full
				ch:send("value3")  -- Will block here
				coroutine.yield("send_unblocked") -- Should only happen after receive
			end)

			-- Second coroutine receives and unblocks first
			coroutine.spawn(function()
				coroutine.yield("receiver_start")
				
				local val1, ok1 = ch:receive()
				assert(ok1 and val1 == "value1", "first receive failed")
				coroutine.yield("received_1")
				
				local val2, ok2 = ch:receive()
				assert(ok2 and val2 == "value2", "second receive failed")
				coroutine.yield("received_2")
				
				local val3, ok3 = ch:receive()
				assert(ok3 and val3 == "value3", "third receive failed")
				coroutine.yield("received_3")
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

		// Verify operation sequence
		expectedYields := []string{
			"receiver_start", // Receiver starts
			"received_1",     // Receives first value
			"received_2",     // Receives second value
			"send_unblocked", // First coroutine unblocks after receiver makes space
			"received_3",     // Receives third value
		}
		assert.Equal(t, expectedYields, yields, "incorrect operation sequence")
	})
}
