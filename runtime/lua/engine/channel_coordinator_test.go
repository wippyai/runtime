package engine

import (
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestChannelCoordinator_Basic(t *testing.T) {
	t.Run("unbuffered send/receive immediate match", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(0) // unbuffered

		// Create send operation
		sendTask := &Task{}
		sendOp := &ChanOperation{
			opType: chanOpSend,
			ch:     ch,
			value:  lua.LString("test"),
		}

		// Create receive operation
		recvTask := &Task{}
		recvOp := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}

		// Add receive first
		tasks := cc.AddOperation(recvTask, recvOp)
		if len(tasks) != 0 {
			t.Fatal("expected receiver to block")
		}

		// Now add send
		tasks = cc.AddOperation(sendTask, sendOp)
		if len(tasks) != 2 {
			t.Fatal("expected both tasks to resume")
		}

		// Verify receive got the value
		if recvTask.resumeVal.String() != "test" {
			t.Errorf("expected receiver to get 'test', got %v", recvTask.resumeVal)
		}

		// Verify send completed successfully
		if sendTask.resumeVal != lua.LBool(true) {
			t.Error("expected send to succeed")
		}
	})

	t.Run("buffered send immediate completion", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(1) // buffered capacity 1

		// Create send operation
		sendTask := &Task{}
		sendOp := &ChanOperation{
			opType: chanOpSend,
			ch:     ch,
			value:  lua.LString("test"),
		}

		// Add send
		tasks := cc.AddOperation(sendTask, sendOp)
		if len(tasks) != 1 {
			t.Fatal("expected send to complete immediately")
		}

		// Verify send completed successfully
		if sendTask.resumeVal != lua.LBool(true) {
			t.Error("expected send to succeed")
		}

		// Verify value is in buffer
		if ch.isEmpty() {
			t.Error("expected value to be in buffer")
		}
	})

	t.Run("closed channel operations", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(0)

		// Queue a sender
		sendTask := &Task{}
		sendOp := &ChanOperation{
			opType: chanOpSend,
			ch:     ch,
			value:  lua.LString("test"),
		}
		tasks := cc.AddOperation(sendTask, sendOp)
		if len(tasks) != 0 {
			t.Fatal("expected send to block")
		}

		// Close the channel
		closeTask := &Task{}
		closeOp := &ChanOperation{
			opType: chanOpClose,
			ch:     ch,
		}
		tasks = cc.AddOperation(closeTask, closeOp)
		if len(tasks) != 2 { // close task + blocked sender
			t.Fatal("expected close to resume all tasks")
		}

		// Verify sender got ChannelClosed
		if sendTask.resumeVal != lua.LNil {
			t.Error("expected send to get ChannelClosed on closed channel")
		}

		// Try to receive from closed channel
		recvTask := &Task{}
		recvOp := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}
		tasks = cc.AddOperation(recvTask, recvOp)
		if len(tasks) != 1 {
			t.Fatal("expected receive to complete immediately")
		}

		// Verify receive got ChannelClosed
		if recvTask.resumeVal != lua.LNil {
			t.Error("expected receive to get ChannelClosed from closed channel")
		}
	})
}

func TestChannelCoordinator_WaitingSender(t *testing.T) {
	t.Run("receiver matches with waiting sender", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(0) // unbuffered

		// Create and queue a sender first
		sendTask := &Task{}
		sendOp := &ChanOperation{
			opType: chanOpSend,
			ch:     ch,
			value:  lua.LString("pending send"),
		}

		// Add send operation - should block
		tasks := cc.AddOperation(sendTask, sendOp)
		if len(tasks) != 0 {
			t.Fatal("expected sender to block")
		}

		// Verify sender was queued
		if cc.senders[ch] == nil {
			t.Fatal("expected sender to be queued")
		}

		// Create and add receive operation
		recvTask := &Task{}
		recvOp := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}

		// Add receive - should match with waiting sender
		tasks = cc.AddOperation(recvTask, recvOp)
		if len(tasks) != 2 {
			t.Fatal("expected both tasks to resume")
		}

		// Verify receive got the value from pending sender
		if recvTask.resumeVal.String() != "pending send" {
			t.Errorf("expected receiver to get 'pending send', got %v", recvTask.resumeVal)
		}

		// Verify send completed successfully
		if sendTask.resumeVal != lua.LBool(true) {
			t.Error("expected send to succeed")
		}

		// Verify sender queue was cleaned up
		if cc.senders[ch] != nil {
			t.Error("expected sender queue to be empty")
		}
	})

	t.Run("multiple waiting senders", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(0) // unbuffered

		// Queue two senders
		sendTask1 := &Task{}
		sendOp1 := &ChanOperation{
			opType: chanOpSend,
			ch:     ch,
			value:  lua.LString("first"),
		}

		sendTask2 := &Task{}
		sendOp2 := &ChanOperation{
			opType: chanOpSend,
			ch:     ch,
			value:  lua.LString("second"),
		}

		// Add sends - both should block
		tasks := cc.AddOperation(sendTask1, sendOp1)
		if len(tasks) != 0 {
			t.Fatal("expected first sender to block")
		}
		tasks = cc.AddOperation(sendTask2, sendOp2)
		if len(tasks) != 0 {
			t.Fatal("expected second sender to block")
		}

		// Create and add receive operation
		recvTask := &Task{}
		recvOp := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}

		// Add receive - should match with first waiting sender
		tasks = cc.AddOperation(recvTask, recvOp)
		if len(tasks) != 2 {
			t.Fatal("expected two tasks to resume")
		}

		// Verify receive got value from first sender
		if recvTask.resumeVal.String() != "first" {
			t.Errorf("expected receiver to get 'first', got %v", recvTask.resumeVal)
		}

		// Verify first send completed successfully
		if sendTask1.resumeVal != lua.LBool(true) {
			t.Error("expected first send to succeed")
		}

		// Verify second sender is still queued
		if cc.senders[ch] == nil {
			t.Error("expected second sender to still be queued")
		}

		if cc.senders[ch].head.op.value.String() != "second" {
			t.Error("wrong sender remained in queue")
		}
	})
}

func TestChannelCoordinator_CloseWithWaitingReceiver(t *testing.T) {
	t.Run("close channel with waiting receiver", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(0) // unbuffered

		// Create and queue a receiver first
		recvTask := &Task{}
		recvOp := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}

		// Add receive operation - should block
		tasks := cc.AddOperation(recvTask, recvOp)
		if len(tasks) != 0 {
			t.Fatal("expected receiver to block")
		}

		// Verify receiver was queued
		if cc.receivers[ch] == nil {
			t.Fatal("expected receiver to be queued")
		}

		// Close the channel
		closeTask := &Task{}
		closeOp := &ChanOperation{
			opType: chanOpClose,
			ch:     ch,
		}

		// Close should resume the waiting receiver
		tasks = cc.AddOperation(closeTask, closeOp)
		if len(tasks) != 2 { // close task + blocked receiver
			t.Fatalf("expected close to resume 2 tasks, got %d", len(tasks))
		}

		// Verify receiver got ChannelClosed
		if recvTask.resumeVal != lua.LNil {
			t.Errorf("expected receiver to get ChannelClosed, got %v", recvTask.resumeVal)
		}

		// Verify receiver queue was cleaned up
		if cc.receivers[ch] != nil {
			t.Error("expected receiver queue to be empty")
		}

		// Verify subsequent receives return ChannelClosed immediately
		newRecvTask := &Task{}
		newRecvOp := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}

		tasks = cc.AddOperation(newRecvTask, newRecvOp)
		if len(tasks) != 1 {
			t.Fatal("expected immediate completion for receive on closed channel")
		}
		if newRecvTask.resumeVal != lua.LNil {
			t.Errorf("expected ChannelClosed from closed channel, got %v", newRecvTask.resumeVal)
		}
	})

	t.Run("close channel with multiple waiting receivers", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(0)

		// Queue multiple receivers
		var recvTasks []*Task
		for i := 0; i < 3; i++ {
			recvTask := &Task{}
			recvOp := &ChanOperation{
				opType: chanOpReceive,
				ch:     ch,
			}
			tasks := cc.AddOperation(recvTask, recvOp)
			if len(tasks) != 0 {
				t.Fatal("expected receiver to block")
			}
			recvTasks = append(recvTasks, recvTask)
		}

		// Close the channel
		closeTask := &Task{}
		closeOp := &ChanOperation{
			opType: chanOpClose,
			ch:     ch,
		}

		tasks := cc.AddOperation(closeTask, closeOp)
		if len(tasks) != 4 { // close task + 3 receivers
			t.Fatalf("expected close to resume 4 tasks, got %d", len(tasks))
		}

		// Verify all receivers got ChannelClosed
		for i, recvTask := range recvTasks {
			if recvTask.resumeVal != lua.LNil {
				t.Errorf("receiver %d: expected ChannelClosed, got %v", i, recvTask.resumeVal)
			}
		}

		// Verify queues are cleaned up
		if cc.receivers[ch] != nil {
			t.Error("expected receiver queue to be empty")
		}
	})
}

func TestChannel_MemoryCleanup(t *testing.T) {
	t.Run("cleanup after close", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(5) // buffered channel

		// Fill buffer
		values := []lua.LValue{
			lua.LString("test1"),
			lua.LString("test2"),
			lua.LString("test3"),
		}

		for _, v := range values {
			ch.send(v)
		}

		// Queue some operations
		sendTask := &Task{}
		sendOp := &ChanOperation{
			opType: chanOpSend,
			ch:     ch,
			value:  lua.LString("pending"),
		}
		cc.AddOperation(sendTask, sendOp)

		recvTask := &Task{}
		recvOp := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}
		cc.AddOperation(recvTask, recvOp)

		// Close channel
		closeTask := &Task{}
		closeOp := &ChanOperation{
			opType: chanOpClose,
			ch:     ch,
		}
		cc.AddOperation(closeTask, closeOp)

		// Verify cleanup
		if ch.buffer != nil {
			t.Error("buffer should be nil after close")
		}
		if ch.size != 0 {
			t.Error("size should be 0 after close")
		}
		if cc.senders[ch] != nil {
			t.Error("senders queue should be cleaned up")
		}
		if cc.receivers[ch] != nil {
			t.Error("receivers queue should be cleaned up")
		}
	})
}

func BenchmarkChannelCoordinator(b *testing.B) {
	b.Run("unbuffered_channel_sync", func(b *testing.B) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(0)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Create send operation
			sendTask := &Task{}
			sendOp := &ChanOperation{
				opType: chanOpSend,
				ch:     ch,
				value:  lua.LNumber(i),
			}

			// Create receive operation
			recvTask := &Task{}
			recvOp := &ChanOperation{
				opType: chanOpReceive,
				ch:     ch,
			}

			// Add receive then send
			cc.AddOperation(recvTask, recvOp)
			cc.AddOperation(sendTask, sendOp)
		}
	})

	b.Run("buffered_channel_send", func(b *testing.B) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(1)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Create send operation
			sendTask := &Task{}
			sendOp := &ChanOperation{
				opType: chanOpSend,
				ch:     ch,
				value:  lua.LNumber(i),
			}

			// Create receive operation to empty buffer
			recvTask := &Task{}
			recvOp := &ChanOperation{
				opType: chanOpReceive,
				ch:     ch,
			}

			// Send then receive
			cc.AddOperation(sendTask, sendOp)
			cc.AddOperation(recvTask, recvOp)
		}
	})

	b.Run("channel_close_with_pending", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cc := NewChannelCoordinator()
			ch := newLuaChannel(0)

			// Queue multiple senders and receivers
			numPending := 10
			for j := 0; j < numPending; j++ {
				sendTask := &Task{}
				sendOp := &ChanOperation{
					opType: chanOpSend,
					ch:     ch,
					value:  lua.LNumber(j),
				}
				cc.AddOperation(sendTask, sendOp)

				recvTask := &Task{}
				recvOp := &ChanOperation{
					opType: chanOpReceive,
					ch:     ch,
				}
				cc.AddOperation(recvTask, recvOp)
			}

			// Close channel with pending operations
			closeTask := &Task{}
			closeOp := &ChanOperation{
				opType: chanOpClose,
				ch:     ch,
			}
			cc.AddOperation(closeTask, closeOp)
		}
	})

	b.Run("mixed_operations", func(b *testing.B) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(5) // medium buffer size

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Alternate between sends, receives, and occasional close/new channel
			switch i % 4 {
			case 0, 1: // Send
				sendTask := &Task{}
				sendOp := &ChanOperation{
					opType: chanOpSend,
					ch:     ch,
					value:  lua.LNumber(i),
				}
				cc.AddOperation(sendTask, sendOp)

			case 2: // Receive
				recvTask := &Task{}
				recvOp := &ChanOperation{
					opType: chanOpReceive,
					ch:     ch,
				}
				cc.AddOperation(recvTask, recvOp)

			case 3: // Close and create new channel
				closeTask := &Task{}
				closeOp := &ChanOperation{
					opType: chanOpClose,
					ch:     ch,
				}
				cc.AddOperation(closeTask, closeOp)
				ch = newLuaChannel(5)
			}
		}
	})
}
