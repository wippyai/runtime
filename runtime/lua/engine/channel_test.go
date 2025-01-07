package engine

import (
	"context"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestChannel_Buffer(t *testing.T) {
	t.Run("unbuffered channel creation", func(t *testing.T) {
		ch := newLuaChannel(0)
		if ch.buffer != nil {
			t.Error("unbuffered channel should have nil buffer")
		}
		if ch.capacity != 0 {
			t.Error("unbuffered channel should have 0 capacity")
		}
	})

	t.Run("buffered channel creation", func(t *testing.T) {
		capacity := 5
		ch := newLuaChannel(capacity)
		if len(ch.buffer) != capacity {
			t.Errorf("buffer length should be %d, got %d", capacity, len(ch.buffer))
		}
		if ch.capacity != capacity {
			t.Errorf("channel capacity should be %d, got %d", capacity, ch.capacity)
		}
	})

	t.Run("circular buffer operations", func(t *testing.T) {
		ch := newLuaChannel(3)

		// Fill buffer
		values := []string{"first", "second", "third"}
		for _, v := range values {
			ok := ch.send(lua.LString(v))
			if !ok {
				t.Error("send should succeed when buffer not full")
			}
		}

		// Buffer should be full
		if !ch.isFull() {
			t.Error("buffer should be full")
		}

		// Try send when full
		ok := ch.send(lua.LString("overflow"))
		if ok {
			t.Error("send should fail when buffer full")
		}

		// Read first value
		val, ok := ch.receive()
		if !ok {
			t.Error("receive should succeed when buffer has values")
		}
		if val.String() != "first" {
			t.Errorf("expected 'first', got %v", val)
		}

		// Should be able to send one more
		ok = ch.send(lua.LString("fourth"))
		if !ok {
			t.Error("send should succeed after receive")
		}

		// Verify circular buffer worked
		remaining := []string{"second", "third", "fourth"}
		for i, expected := range remaining {
			val, ok := ch.receive()
			if !ok {
				t.Errorf("receive %d should succeed", i)
			}
			if val.String() != expected {
				t.Errorf("expected '%s', got %v", expected, val)
			}
		}

		// Should be empty now
		if !ch.isEmpty() {
			t.Error("buffer should be empty")
		}
	})

	t.Run("closed channel operations", func(t *testing.T) {
		ch := newLuaChannel(3)

		// Fill partially
		ch.send(lua.LString("value"))

		// Close channel
		ch.closed = true

		// Try operations on closed channel
		ok := ch.send(lua.LString("after close"))
		if ok {
			t.Error("send should fail on closed channel")
		}

		// Should still be able to receive buffered values
		val, ok := ch.receive()
		if !ok {
			t.Error("receive should get buffered value from closed channel")
		}
		if val.String() != "value" {
			t.Errorf("wrong value received, got %v", val)
		}

		// Further receives should fail
		val, ok = ch.receive()
		if ok {
			t.Error("receive should fail when closed channel is empty")
		}
	})

	t.Run("buffer wrapping", func(t *testing.T) {
		ch := newLuaChannel(3)
		sequence := []string{
			"1", "2", "3", // Fill buffer
			"4", "5", "6", // Replace all values
			"7", "8", "9", // Replace all again
		}

		// Send values in groups of 3
		for i := 0; i < len(sequence); i += 3 {
			// Receive previous values if not first group
			if i > 0 {
				for j := 0; j < 3; j++ {
					val, ok := ch.receive()
					if !ok {
						t.Errorf("receive failed at i=%d, j=%d", i, j)
					}
					expected := sequence[i-3+j]
					if val.String() != expected {
						t.Errorf("expected '%s', got %v", expected, val)
					}
				}
			}

			// Send new values
			for j := 0; j < 3; j++ {
				ok := ch.send(lua.LString(sequence[i+j]))
				if !ok {
					t.Errorf("send failed at i=%d, j=%d", i, j)
				}
			}
		}

		// Verify final values
		for i := 6; i < 9; i++ {
			val, ok := ch.receive()
			if !ok {
				t.Errorf("final receive failed at i=%d", i)
			}
			if val.String() != sequence[i] {
				t.Errorf("expected '%s', got %v", sequence[i], val)
			}
		}

		// Buffer should be empty with indices wrapped around
		if !ch.isEmpty() {
			t.Error("buffer should be empty")
		}
		if ch.read != ch.write {
			t.Error("read and write indices should be equal when empty")
		}
	})

	t.Run("verify fixed capacity", func(t *testing.T) {
		capacity := 2
		ch := newLuaChannel(capacity)

		// Fill to capacity
		if !ch.send(lua.LString("1")) {
			t.Error("first send should succeed")
		}
		if !ch.send(lua.LString("2")) {
			t.Error("second send should succeed")
		}

		// Try to exceed capacity
		if ch.send(lua.LString("3")) {
			t.Error("send should fail when buffer at capacity")
		}

		// Verify size hasn't grown
		if ch.size != capacity {
			t.Errorf("buffer size should be %d, got %d", capacity, ch.size)
		}
		if len(ch.buffer) != capacity {
			t.Errorf("underlying buffer should remain size %d, got %d", capacity, len(ch.buffer))
		}
	})
}

func TestChannelVM_Basic(t *testing.T) {
	logger := zap.NewNop()

	t.Run("unbuffered channel send/receive", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.DoString(`
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
		tasks, _ := vm.Step()
		assert.Equal(t, 2, len(tasks), "expected 2 yielded tasks")

		// Step all tasks until completion
		for len(tasks) > 0 {
			var err error
			tasks, err = vm.Step(tasks...)
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("buffered channel", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.DoString(`
			local ch = channel.new(1)  -- buffered channel with capacity 1
	
			-- Sender can complete immediately
			coroutine.spawn(function()
				ch:send("buffered")
				coroutine.yield("send_complete")
			end)
	
			-- Receiver gets buffered value
			coroutine.spawn(function()
				local msg, ok = ch:receive()
				assert(ok, "expected successful receive")
				assert(msg == "buffered", "wrong message received")
				coroutine.yield("receive_complete")
			end)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()

		// Step all tasks until completion
		for len(tasks) > 0 {
			var err error
			tasks, err = vm.Step(tasks...)
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("closed channel", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.DoString(`
			local ch = channel.new(0)
	
			-- Receiver task
			coroutine.spawn(function()
				ch:close()
				local msg, ok = ch:receive()
				assert(not ok, "expected closed channel receive")
				assert(msg == nil, "expected nil from closed channel")
				coroutine.yield("receive_after_close")
			end)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		tasks := vm.GetYieldedTasks()
		for len(tasks) > 0 {
			var err error
			tasks, err = vm.Step(tasks...)
			if err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestChannelVM_Operations(t *testing.T) {
	logger := zap.NewNop()

	t.Run("unbuffered channel yield sequence", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Track all yields in order
		var yields []string

		err = vm.DoString(`
			-- Create an unbuffered channel
			local ch = channel.new(0)
			
			-- Sender coroutine
			coroutine.spawn(function()
				coroutine.yield("sender_start")
				ch:send("message1")
				coroutine.yield("sender_after_send1")
				ch:send("message2")
				coroutine.yield("sender_after_send2")
				return "sender_done"
			end)
			
			-- Receiver coroutine
			coroutine.spawn(function()
				coroutine.yield("receiver_start")
				local msg1 = ch:receive()
				coroutine.yield("receiver_got_" .. msg1)
				local msg2 = ch:receive()
				coroutine.yield("receiver_got_" .. msg2)
				return "receiver_done"
			end)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		// Start the coroutines
		initialTasks, err := vm.Step()
		assert.NoError(t, err)
		assert.Equal(t, 2, len(initialTasks), "expected both coroutines to yield initially")

		// Initialize tasks with the spawned coroutines
		tasks := initialTasks

		for len(tasks) > 0 {
			var nextTasks []*Task
			for _, task := range tasks {
				// Record current yields before stepping
				if vals := task.GetYieldedValues(); len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}

				moreTasks, err := vm.Step(task)
				assert.NoError(t, err)
				nextTasks = append(nextTasks, moreTasks...)
			}
			tasks = nextTasks
		}

		// Verify yield sequence
		expectedYields := []string{
			"sender_start",
			"receiver_start",
			"sender_after_send1",
			"receiver_got_message1",
			"sender_after_send2",
			"receiver_got_message2",
		}

		assert.Equal(t, expectedYields, yields, "incorrect yield sequence")
	})

	t.Run("buffered channel yield sequence", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		assert.NoError(t, err)
		defer vm.Close()

		var yields []string

		err = vm.DoString(`
			local ch = channel.new(1)  -- Buffer size 1
			
			-- Sender fills buffer then blocks
			coroutine.spawn(function()
				coroutine.yield("sender_start")
				ch:send("msg1")
				coroutine.yield("sender_after_send1")
				ch:send("msg2")  -- This will block until first message is received
				coroutine.yield("sender_after_send2")
				ch:send("msg3")  -- This will block until second message is received
				coroutine.yield("sender_after_send3")
				return "sender_done"
			end)

			-- Receiver coroutine
			coroutine.spawn(function()
				coroutine.yield("receiver_start")
				local msg = ch:receive()  -- Gets msg1
				coroutine.yield("receiver_got_" .. msg)
				msg = ch:receive()  -- Gets msg2
				coroutine.yield("receiver_got_" .. msg)
				msg = ch:receive()  -- Gets msg3
				coroutine.yield("receiver_got_" .. msg)
				return "receiver_done"
			end)
		`, "test")

		assert.NoError(t, err)

		// Start the coroutines
		initialTasks, err := vm.Step()
		assert.NoError(t, err)
		assert.Equal(t, 2, len(initialTasks), "expected both coroutines to yield initially")

		// Initialize tasks with the spawned coroutines
		tasks := initialTasks

		for len(tasks) > 0 {
			var nextTasks []*Task
			for _, task := range tasks {
				// Record current yields before stepping
				if vals := task.GetYieldedValues(); len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}

				moreTasks, err := vm.Step(task)
				assert.NoError(t, err)
				nextTasks = append(nextTasks, moreTasks...)
			}
			tasks = nextTasks
		}

		// Verify yield sequence
		expectedYields := []string{
			"sender_start",
			"receiver_start",
			"sender_after_send1", // First send succeeds immediately (buffer size 1)
			"receiver_got_msg1",  // First receive
			"sender_after_send2", // Second send completes after first receive
			"receiver_got_msg2",  // Second receive
			"sender_after_send3", // Third send completes after second receive
			"receiver_got_msg3",  // Third receive
		}

		assert.Equal(t, expectedYields, yields, "incorrect yield sequence")
	})

	t.Run("channel close yield sequence", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		assert.NoError(t, err)
		defer vm.Close()

		var yields []string

		err = vm.DoString(`
			local ch = channel.new(1)  -- Buffer size 1
			
			-- Multiple receivers
			for i = 1, 2 do
				coroutine.spawn(function()
					coroutine.yield("receiver" .. i .. "_start")
					local msg, ok = ch:receive()
					local result
					if ok and msg then
						result = "receiver" .. i .. "_got_" .. msg
					else
						result = "receiver" .. i .. "_got_closed"
					end
					coroutine.yield(result)
					return "receiver" .. i .. "_done"
				end)
			end

			-- Sender that closes
			coroutine.spawn(function()
				coroutine.yield("sender_start")
				ch:send("msg1")
				coroutine.yield("sender_after_send")
				ch:close()
				coroutine.yield("sender_after_close")
				local ok, err = pcall(function()
					ch:send("msg2")  -- Should fail
				end)
				coroutine.yield("sender_after_failed_send")
				return "sender_done"
			end)
		`, "test")

		assert.NoError(t, err)

		// Start the coroutines
		initialTasks, err := vm.Step()
		assert.NoError(t, err)
		assert.Equal(t, 3, len(initialTasks), "expected both coroutines to yield initially")

		// Process all tasks until completion
		tasks := initialTasks
		for len(tasks) > 0 {
			var nextTasks []*Task
			for _, task := range tasks {
				if vals := task.GetYieldedValues(); len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}

				moreTasks, err := vm.Step(task)
				assert.NoError(t, err)
				nextTasks = append(nextTasks, moreTasks...)
			}
			tasks = nextTasks
		}

		// Verify yield sequence
		expectedYields := []string{
			"receiver1_start",
			"receiver2_start",
			"sender_start",
			"sender_after_send",
			"receiver1_got_msg1",       // First receiver gets the buffered message
			"receiver2_got_closed",     // Second receiver sees closed channel
			"sender_after_close",       // Channel gets closed (due to scheduling triggered after)
			"sender_after_failed_send", // Send after close fails
		}

		assert.Equal(t, expectedYields, yields, "incorrect yield sequence")
	})

	t.Run("multiple senders and receivers", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		assert.NoError(t, err)
		defer vm.Close()

		var yields []string

		err = vm.DoString(`
			local ch = channel.new(1)  -- Buffer size 1
			
			-- Multiple senders
			for i = 1, 2 do
				coroutine.spawn(function()
					coroutine.yield("sender" .. i .. "_start")
					ch:send("msg" .. i)
					coroutine.yield("sender" .. i .. "_after_send")
					return "sender" .. i .. "_done"
				end)
			end
			
			-- Multiple receivers
			for i = 1, 2 do
				coroutine.spawn(function()
					coroutine.yield("receiver" .. i .. "_start")
					local msg = ch:receive()
					coroutine.yield("receiver" .. i .. "_got_" .. msg)
					return "receiver" .. i .. "_done"
				end)
			end
		`, "test")

		assert.NoError(t, err)

		// Start the coroutines
		initialTasks, err := vm.Step()
		assert.NoError(t, err)
		assert.Equal(t, 4, len(initialTasks), "expected four coroutines to yield initially")

		// Process all tasks until completion
		tasks := initialTasks
		for len(tasks) > 0 {
			var nextTasks []*Task
			for _, task := range tasks {
				if vals := task.GetYieldedValues(); len(vals) > 0 {
					yields = append(yields, vals[0].String())
				}

				moreTasks, err := vm.Step(task)
				assert.NoError(t, err)
				nextTasks = append(nextTasks, moreTasks...)
			}
			tasks = nextTasks
		}

		// Verify initial yields and completion yields
		assert.Contains(t, yields, "sender1_start")
		assert.Contains(t, yields, "sender2_start")
		assert.Contains(t, yields, "receiver1_start")
		assert.Contains(t, yields, "receiver2_start")

		// Verify that first message was received before second send completed
		assertOrderedYields(t, yields,
			"sender1_after_send",
			"receiver1_got_msg1",
			"sender2_after_send",
			"receiver2_got_msg2")
	})
}

// Helper to verify that yields appear in the specified order
func assertOrderedYields(t *testing.T, yields []string, orderedYields ...string) {
	lastIdx := -1
	for _, yield := range orderedYields {
		idx := -1
		for i, y := range yields {
			if y == yield {
				idx = i
				break
			}
		}
		if idx == -1 {
			t.Errorf("yield %q not found in sequence", yield)
			return
		}
		if idx <= lastIdx {
			t.Errorf("yield %q found at position %d, expected after position %d", yield, idx, lastIdx)
			return
		}
		lastIdx = idx
	}
}
