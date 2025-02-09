package channel

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestUnbufferedChannelOperations(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
			-- Create an unbuffered channel
			local ch = channel.new()

			-- Sender coroutine
			coroutine.spawn(function()
				coroutine.yield("sender_start")
				ch:send("message") -- blocks
				coroutine.yield("sent")
			end)

			-- Receiver coroutine
			coroutine.spawn(function()
				coroutine.yield("receiver_start")
				local msg, ok = ch:receive()
				assert(msg == "message", "wrong message received: " .. tostring(msg))
				assert(ok == true, "receive should succeed")
				coroutine.yield("received")
			end)
		`, "test")
	assert.NoError(t, err)

	channels := NewChannelLayer()
	tasks, err := channels.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = channels.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"sender_start",
		"receiver_start",
		"sent", // goes in order of routine registration
		"received",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestUnbufferedChannelOperationsMainCoroutine(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
			-- Create an unbuffered channel
			local ch = channel.new()

			-- Sender coroutine
			coroutine.spawn(function()
				coroutine.yield("sender_start")
				ch:send("message") -- blocks
				coroutine.yield("sent")
			end)

			local msg, ok = ch:receive()
			assert(msg == "message", "wrong message received: " .. tostring(msg))
			assert(ok == true, "receive should succeed")
			coroutine.yield("received")	
		`, "test")
	assert.NoError(t, err)

	channels := NewChannelLayer()
	tasks, err := channels.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = channels.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"sender_start",
		"received",
		"sent", // remember, this is separate yield by the sender coroutine
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestClosedChannelOperations(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
			-- Create a channel and close it
			local ch = channel.new()
			ch:close()

			-- Try receiving from closed channel
			coroutine.spawn(function()
				coroutine.yield("receiver_start")
				local msg, ok = ch:receive()
				assert(msg == nil, "expected nil message")
				assert(ok == false, "expected receive failure")
				coroutine.yield("receive_done")
			end)

			-- Try sending to closed channel
			coroutine.spawn(function()
				coroutine.yield("sender_start")
				local success, err = pcall(function()
					ch:send("message")
				end)
				assert(not success, "send should fail")
				assert(string.match(err, "send on closed channel"), "wrong error message")
				coroutine.yield("send_done")
			end)
		`, "test")
	assert.NoError(t, err)

	runtime := NewChannelLayer()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"receiver_start",
		"sender_start",
		"receive_done",
		"send_done",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestCloseChannelWithPendingOperations(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
			-- Create a channel
			local ch = channel.new()

			-- Create blocking receiver first
			coroutine.spawn(function()
				coroutine.yield("receiver_start")
				local msg, ok = ch:receive()  -- This will block
				assert(msg == nil, "expected nil message")
				assert(ok == false, "expected receive failure")
				coroutine.yield("receiver_notified")
			end)

			-- Close the channel after receiver is blocked
			coroutine.spawn(function()
				coroutine.yield("closer_start")
				ch:close()
				coroutine.yield("channel_closed")
			end)
		`, "test")
	assert.NoError(t, err)

	channels := NewChannelLayer()
	tasks, err := channels.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = channels.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"receiver_start",
		"closer_start",
		"receiver_notified",
		"channel_closed", // this is normal, we expect to wake up close AFTER propagating close to receivers
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestBufferedChannelBasicOperations(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
			-- Create a buffered channel with capacity 2
			local ch = channel.new(2)

			-- Test non-blocking sends up to capacity
			coroutine.spawn(function()
				ch:send("msg1") -- no block
				coroutine.yield("first_sent")
				ch:send("msg2") -- no block
				coroutine.yield("second_sent")
			end)

			-- Test block from buffer
			coroutine.spawn(function()
				coroutine.yield("receiver_start")
				local msg1, ok1 = ch:receive() -- no block
				assert(msg1 == "msg1" and ok1 == true, "first receive failed")
				coroutine.yield("first_received")

				local msg2, ok2 = ch:receive() -- no block
				assert(msg2 == "msg2" and ok2 == true, "second receive failed")
				coroutine.yield("second_received")
			end)
		`, "test")
	assert.NoError(t, err)

	channels := NewChannelLayer()
	tasks, err := channels.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = channels.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"first_sent",
		"receiver_start",
		"second_sent",
		"first_received",
		"second_received",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}
func TestBufferedChannelBlockingBehavior(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		-- Create a buffered channel with capacity 1
		local ch = channel.new(1)

		-- Fill the buffer and attempt to send (should block)
		coroutine.spawn(function()
			ch:send("msg1") -- no block
			coroutine.yield("buffer_written")

			-- This send should block until receiver gets msg1
			ch:send("msg2")
			coroutine.yield("blocked_send_complete")
		end)

		-- Receiver gets values after sender blocks
		coroutine.spawn(function()
			coroutine.yield("receiver_start")

			local msg1, ok1 = ch:receive()
			assert(msg1 == "msg1" and ok1 == true, "first receive failed")
			coroutine.yield("first_received")

			local msg2, ok2 = ch:receive()
			assert(msg2 == "msg2" and ok2 == true, "second receive failed")
			coroutine.yield("second_received")
		end)
	`, "test")
	assert.NoError(t, err)

	channels := NewChannelLayer()
	tasks, err := channels.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = channels.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"buffer_written",
		"receiver_start",
		"first_received",
		"blocked_send_complete",
		"second_received",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestReadBufferedValues(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		-- Create a buffered channel with capacity 1
		local ch = channel.new(1)
		
		ch:send("msg1") -- no block
		ch:close() -- no block

		coroutine.yield("read_start")

		local msg1, ok1 = ch:receive()
		assert(msg1 == "msg1" and ok1 == true, "should receive buffered message")
		coroutine.yield("buffered_received")
		
		local msg2, ok2 = ch:receive()
		assert(msg2 == nil and ok2 == false, "should get closed channel signal")
		coroutine.yield("done")
	`, "test")
	assert.NoError(t, err)

	channels := NewChannelLayer()
	tasks, err := channels.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = channels.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"read_start",
		"buffered_received",
		"done",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestBufferedChannelEdgeCases(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		-- Test error cases first
		local success, err = pcall(function()
			local invalidCh = channel.new(-1) -- Should error
		end)
		assert(not success, "negative capacity should fail")
		coroutine.yield("invalid_capacity_checked")
		
		-- Test sending multiple values and ordering in buffered channel
		local chMulti = channel.new(3)
		
		-- Multiple sends first (all should succeed without blocking)
		chMulti:send("msg1")
		coroutine.yield("sent1")
		chMulti:send("msg2")
		coroutine.yield("sent2")
		chMulti:send("msg3")
		coroutine.yield("sent3")
		
		-- Now receive all values in order
		local msg1, ok1 = chMulti:receive()
		assert(msg1 == "msg1", "wrong first message")
		coroutine.yield("received1")
		
		local msg2, ok2 = chMulti:receive()
		assert(msg2 == "msg2", "wrong second message")
		coroutine.yield("received2")
		
		local msg3, ok3 = chMulti:receive()
		assert(msg3 == "msg3", "wrong third message")
		coroutine.yield("received3")
		
		-- Test close with buffered values
		local chClose = channel.new(2)
		chClose:send("buffered1")
		chClose:send("buffered2")
		chClose:close()
		
		local msg1, ok1 = chClose:receive()
		assert(msg1 == "buffered1" and ok1 == true, "should receive first buffered value")
		coroutine.yield("close_buffered1")
		
		local msg2, ok2 = chClose:receive()
		assert(msg2 == "buffered2" and ok2 == true, "should receive second buffered value")
		coroutine.yield("close_buffered2")
		
		local msg3, ok3 = chClose:receive()
		assert(msg3 == nil and ok3 == false, "should get closed signal after buffered values")
		coroutine.yield("close_with_buffer_checked")
	`, "test")
	assert.NoError(t, err)

	channels := NewChannelLayer()
	tasks, err := channels.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = channels.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"invalid_capacity_checked",
		"sent1",
		"sent2",
		"sent3",
		"received1",
		"received2",
		"received3",
		"close_buffered1",
		"close_buffered2",
		"close_with_buffer_checked",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestBufferedChannelCloseWithPendingOperations(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		-- Create a buffered channel with capacity 2
		local ch = channel.new(2)

		-- Fill the buffer (no blocking)
		ch:send("msg1")
		coroutine.yield("first_buffered")
		ch:send("msg2") 
		coroutine.yield("second_buffered")

		-- First receiver gets first buffered value
		local msg1, ok1 = ch:receive()
		assert(msg1 == "msg1" and ok1 == true, "should receive first buffered message")
		coroutine.yield("received_first")

		-- Second receiver gets second buffered value 
		local msg2, ok2 = ch:receive()
		assert(msg2 == "msg2" and ok2 == true, "should receive second buffered message")
		coroutine.yield("received_second")

		-- Close empty channel 
		ch:close()
		coroutine.yield("channel_closed")

		-- Receive from closed empty channel
		local msg3, ok3 = ch:receive()
		assert(msg3 == nil and ok3 == false, "should get closed channel signal")
		coroutine.yield("received_closed")
	`, "test")
	assert.NoError(t, err)

	channels := NewChannelLayer()
	tasks, err := channels.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = channels.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"first_buffered",
		"second_buffered",
		"received_first",
		"received_second",
		"channel_closed",
		"received_closed",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestBufferedChannelClose(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		local ch = channel.new(2)
		
		-- Buffer a value
		ch:send("msg1")
		coroutine.yield("buffered")
		
		-- Close with value still buffered
		ch:close() 
		coroutine.yield("closed")

		-- GetField buffered value after close
		local msg, ok = ch:receive()
		assert(msg == "msg1" and ok == true, "should get buffered value")
		coroutine.yield("received_buffered")

		-- GetField closed signal
		local msg2, ok2 = ch:receive()
		assert(msg2 == nil and ok2 == false, "should get closed signal")
		coroutine.yield("received_closed")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelLayer()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"buffered",
		"closed",
		"received_buffered",
		"received_closed",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestBufferedChannelSendError(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		local ch = channel.new(1)
		ch:close()
		ch:send("msg") -- Should error
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelLayer()
	_, err = runtime.Step(vm)
	if err == nil {
		t.Error("expected error from send on closed channel")
	} else if !strings.Contains(err.Error(), "send on closed channel") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestMainCoroutineBlockingOnBufferedChannel verifies that the main coroutine
// properly blocks when sending to a full buffered channel
func TestMainCoroutineBlockingOnBufferedChannel(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		-- Create a buffered channel with capacity 1
		local ch = channel.new(1)
		
		-- Fill the buffer
		ch:send("msg1")
		coroutine.yield("buffer_filled")
		
		-- This should block the main coroutine
		ch:send("msg2")
		coroutine.yield("never_reached")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelLayer()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"buffer_filled",
	}
	assert.Equal(t, expectedOrder, yields, "main coroutine should block after buffer is full")
}

// TestMainCoroutinePanicHandling verifies that channel operations properly
// handle panics in the main coroutine
func TestMainCoroutinePanicHandling(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		local ch = channel.new(0)
		
		-- Start a goroutine that will be blocked
		coroutine.spawn(function()
			ch:receive()
		end)
		
		coroutine.yield("goroutine_started")
		
		-- Cause a panic in main coroutine
		error("deliberate panic")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelLayer()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		if err != nil {
			// Expected panic
			break
		}
	}

	expectedOrder := []string{
		"goroutine_started",
	}
	assert.Equal(t, expectedOrder, yields)
}

// TestMainCoroutineChannelCascadingClose tests that when main coroutine
// closes a channel, all blocked operations are properly cleaned up
func TestMainCoroutineChannelCascadingClose(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		local ch = channel.new(0)
		local next = channel.new(2) -- Collect next from goroutines
		
		-- Start two goroutines that will be blocked on receive
		for i = 1, 2 do
			coroutine.spawn(function()
				local val, ok = ch:receive()
				next:send({value = val, ok = ok})
			end)
		end
		
		coroutine.yield("goroutines_started")
		
		-- Close channel from main coroutine
		ch:close()
		coroutine.yield("channel_closed")
		
		-- Verify both goroutines received close notification
		local result1 = next:receive()
		assert(result1.value == nil and result1.ok == false, "first goroutine should get close signal")
		coroutine.yield("first_result_checked")
		
		local result2 = next:receive()
		assert(result2.value == nil and result2.ok == false, "second goroutine should get close signal")
		coroutine.yield("second_result_checked")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelLayer()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"goroutines_started",
		"channel_closed",
		"first_result_checked",
		"second_result_checked",
	}
	assert.Equal(t, expectedOrder, yields)
}

func TestMapReducePattern(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		-- Create channels for work distribution and result collection
		local workCh = channel.new(5)    -- Buffer some work items
		local resultCh = channel.new(0)  -- Unbuffered next channel
		local doneCh = channel.new(0)    -- Synchronization channel

		-- Input data: numbers to be processed
		local input = {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		local expectedSum = 385  -- Sum of squares of numbers 1-10

		-- Start workers
		for i = 1, 3 do
			coroutine.spawn(function()
				while true do
					local num, ok = workCh:receive()
					if not ok then
						coroutine.yield("worker_" .. i .. "_done")
						break
					end
					-- Square the number and send result
					resultCh:send({
						worker = i,
						input = num,
						result = num * num
					})
					coroutine.yield("worker_" .. i .. "_processed_" .. num)
				end
			end)
		end

		-- Producer that distributes work
		coroutine.spawn(function()
			for _, num in ipairs(input) do
				workCh:send(num)
				coroutine.yield("distributed_" .. num)
			end
			workCh:close()
			coroutine.yield("distribution_complete")
		end)

		-- Consumer that reduces next
		coroutine.spawn(function()
			local next = {}
			local sum = 0

			-- Collect and reduce next
			while #next < #input do
				local result = resultCh:receive()
				table.insert(next, result)
				sum = sum + result.result
				coroutine.yield("reduced_" .. result.input)
			end

			assert(sum == expectedSum, string.format(
				"wrong sum: expected %d, got %d", expectedSum, sum))
			
			resultCh:close()
			doneCh:send(true)
			coroutine.yield("reduce_complete")
		end)

		-- wait for completion
		doneCh:receive()
		coroutine.yield("all_complete")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelLayer()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	// Verify expected sequence points
	assert.Contains(t, yields, "distribution_complete")
	assert.Contains(t, yields, "reduce_complete")
	assert.Contains(t, yields, "all_complete")

	// Verify all workers completed
	assert.Contains(t, yields, "worker_1_done")
	assert.Contains(t, yields, "worker_2_done")
	assert.Contains(t, yields, "worker_3_done")

	// Verify all numbers were processed
	for i := 1; i <= 10; i++ {
		distributedFound := false
		reducedFound := false
		for _, y := range yields {
			if y == fmt.Sprintf("distributed_%d", i) {
				distributedFound = true
			}
			if y == fmt.Sprintf("reduced_%d", i) {
				reducedFound = true
			}
		}
		assert.True(t, distributedFound, "number %d was not distributed", i)
		assert.True(t, reducedFound, "number %d was not reduced", i)
	}
}

func TestFanOutPattern(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		-- Input channel and multiple output channels
		local source = channel.new(0)
		local outputs = {
			channel.new(0),
			channel.new(0),
			channel.new(0)
		}
		local doneCh = channel.new(0)
		
		-- Fan-out distributor
		coroutine.spawn(function()
			local outIdx = 1
			for i = 1, 6 do
				-- Round-robin distribution
				outputs[outIdx]:send(i)
				coroutine.yield("distributed_" .. i .. "_to_" .. outIdx)
				outIdx = (outIdx % #outputs) + 1
			end
			
			-- Close all output channels
			for i = 1, #outputs do
				outputs[i]:close()
			end
			coroutine.yield("outputs_closed")
		end)
		
		-- Consumers for each output channel
		local received = {0, 0, 0}
		for i = 1, #outputs do
			coroutine.spawn(function()
				while true do
					local val, ok = outputs[i]:receive()
					if not ok then
						break
					end
					received[i] = received[i] + 1
					coroutine.yield("consumer_" .. i .. "_received_" .. val)
				end
				coroutine.yield("consumer_" .. i .. "_done")
				
				-- Signal completion
				if i == #outputs then
					-- Verify distribution
					assert(received[1] + received[2] + received[3] == 6,
						"wrong number of items processed")
					doneCh:send(true)
				end
			end)
		end
		
		-- wait for completion
		doneCh:receive()
		coroutine.yield("all_complete")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelLayer()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	// Verify distribution completed
	assert.Contains(t, yields, "outputs_closed")
	assert.Contains(t, yields, "all_complete")

	// Verify all consumers completed
	assert.Contains(t, yields, "consumer_1_done")
	assert.Contains(t, yields, "consumer_2_done")
	assert.Contains(t, yields, "consumer_3_done")

	// TODO: Verify round-robin distribution (distribution order is not used or tested)
	// distributionOrder := make([]string, 0)
	// for _, yield := range yields {
	//	if strings.HasPrefix(yield, "distributed_") {
	//		distributionOrder = append(distributionOrder, yield)
	//	}
	// }

	expectedDistribution := []string{
		"distributed_1_to_1",
		"distributed_2_to_2",
		"distributed_3_to_3",
		"distributed_4_to_1",
		"distributed_5_to_2",
		"distributed_6_to_3",
	}

	// Verify distribution sequence
	for i, expected := range expectedDistribution {
		assert.Contains(t, yields, expected, "missing distribution %s", expected)
		if i > 0 {
			// Find positions in yields slice to verify order
			prevPos := -1
			currPos := -1
			for j, y := range yields {
				if y == expectedDistribution[i-1] {
					prevPos = j
				}
				if y == expected {
					currPos = j
				}
			}
			assert.Less(t, prevPos, currPos,
				"wrong distribution order between %s and %s",
				expectedDistribution[i-1], expected)
		}
	}
}

func TestFanInPattern(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.StartString(context.Background(), `
		-- Multiple input channels and single output channel
		local inputs = {
			channel.new(0),
			channel.new(0),
			channel.new(0)
		}
		local output = channel.new(0)
		local doneCh = channel.new(0)
		
		-- Producers for each input channel
		for i = 1, #inputs do
			coroutine.spawn(function()
				-- Each producer sends its index twice
				inputs[i]:send(i)
				coroutine.yield("producer_" .. i .. "_first")
				inputs[i]:send(i)
				coroutine.yield("producer_" .. i .. "_second")
				inputs[i]:close()
				coroutine.yield("producer_" .. i .. "_done")
			end)
		end
		
		-- Fan-in multiplexer
		coroutine.spawn(function()
			local active = {true, true, true}
			local count = 0
			local next = {}
			
			while count < 6 do  -- Total of 6 items expected
				-- Try receive from all active channels
				for i = 1, #inputs do
					if active[i] then
						local val, ok = inputs[i]:receive()
						if not ok then
							active[i] = false
							coroutine.yield("input_" .. i .. "_closed")
						else
							output:send({source = i, value = val})
							count = count + 1
							coroutine.yield("multiplexed_from_" .. i)
						end
					end
				end
			end
			
			output:close()
			coroutine.yield("multiplexer_complete")
		end)
		
		-- Consumer
		coroutine.spawn(function()
			local received = {}
			
			while true do
				local val, ok = output:receive()
				if not ok then
					break
				end
				table.insert(received, val)
				coroutine.yield("consumer_received_" .. val.value .. "_from_" .. val.source)
			end
			
			-- Verify we got all expected values
			assert(#received == 6, "wrong number of items received")
			
			-- Count occurrences from each source
			local counts = {0, 0, 0}
			for _, v in ipairs(received) do
				counts[v.source] = counts[v.source] + 1
			end
			
			-- Verify each source contributed twice
			for i = 1, 3 do
				assert(counts[i] == 2, 
					string.format("source %d contributed %d times, expected 2", 
						i, counts[i]))
			end
			
			doneCh:send(true)
			coroutine.yield("consumer_complete")
		end)
		
		-- wait for completion
		doneCh:receive()
		coroutine.yield("all_complete")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelLayer()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	// Verify completion states
	assert.Contains(t, yields, "multiplexer_complete")
	assert.Contains(t, yields, "consumer_complete")
	assert.Contains(t, yields, "all_complete")

	// Verify all producers completed
	for i := 1; i <= 3; i++ {
		assert.Contains(t, yields, fmt.Sprintf("producer_%d_first", i))
		assert.Contains(t, yields, fmt.Sprintf("producer_%d_second", i))
		assert.Contains(t, yields, fmt.Sprintf("producer_%d_done", i))
	}

	// Verify multiplexing occurred
	multiplexCount := 0
	for _, yield := range yields {
		if strings.HasPrefix(yield, "multiplexed_from_") {
			multiplexCount++
		}
	}
	assert.Equal(t, 6, multiplexCount, "wrong number of multiplexed items")

	// Verify consumer received all items
	receiveCount := 0
	for _, yield := range yields {
		if strings.HasPrefix(yield, "consumer_received_") {
			receiveCount++
		}
	}
	assert.Equal(t, 6, receiveCount, "wrong number of received items")
}
