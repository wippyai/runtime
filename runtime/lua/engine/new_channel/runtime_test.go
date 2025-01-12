package channel

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"strings"
	"testing"
)

func TestUnbufferedChannelOperations(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer func() { _ = vm.Close() }()

	err = vm.PushScript(`
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

	runtime := NewRuntime()
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
		"sender_start",
		"receiver_start",
		"sent", // goes in order of routine registration
		"received",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestUnbufferedChannelOperationsMainCoroutine(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer func() { _ = vm.Close() }()

	err = vm.PushScript(`
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

	runtime := NewRuntime()
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
		"sender_start",
		"received",
		"sent", // remember, this is separate yield by the sender coroutine
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestClosedChannelOperations(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
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

	runtime := NewRuntime()
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

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
			-- Create a channel
			local ch = channel.new()

			-- Spawn blocking receiver first
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

	runtime := NewRuntime()
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
		"closer_start",
		"receiver_notified",
		"channel_closed", // this is normal, we expect to wake up close AFTER propagating close to receivers
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestBufferedChannelBasicOperations(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
			-- Create a buffered channel with capacity 2
			local ch = channel.new(2)

			-- Test non-blocking sends up to capacity
			coroutine.spawn(function()
				ch:send("msg1") -- no block
				coroutine.yield("first_sent")
				ch:send("msg2") -- no block
				coroutine.yield("second_sent")
			end)

			-- Test receives from buffer
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

	runtime := NewRuntime()
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

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
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

	runtime := NewRuntime()
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

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
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

	runtime := NewRuntime()
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
		"read_start",
		"buffered_received",
		"done",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestBufferedChannelEdgeCases(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
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

	runtime := NewRuntime()
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

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
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

	runtime := NewRuntime()
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

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		local ch = channel.new(2)
		
		-- Buffer a value
		ch:send("msg1")
		coroutine.yield("buffered")
		
		-- Close with value still buffered
		ch:close() 
		coroutine.yield("closed")

		-- Get buffered value after close
		local msg, ok = ch:receive()
		assert(msg == "msg1" and ok == true, "should get buffered value")
		coroutine.yield("received_buffered")

		-- Get closed signal
		local msg2, ok2 = ch:receive()
		assert(msg2 == nil and ok2 == false, "should get closed signal")
		coroutine.yield("received_closed")
	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
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

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		local ch = channel.new(1)
		ch:close()
		ch:send("msg") -- Should error
	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	_, err = runtime.Step(vm)
	if err == nil {
		t.Error("expected error from send on closed channel")
	} else if !strings.Contains(err.Error(), "send on closed channel") {
		t.Errorf("unexpected error: %v", err)
	}
}
