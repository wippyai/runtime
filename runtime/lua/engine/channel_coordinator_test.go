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
		tasks := cc.PushOperation(recvTask, recvOp)
		if len(tasks) != 0 {
			t.Fatal("expected receiver to block")
		}

		// Now add send
		tasks = cc.PushOperation(sendTask, sendOp)
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
		tasks := cc.PushOperation(sendTask, sendOp)
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
		tasks := cc.PushOperation(sendTask, sendOp)
		if len(tasks) != 0 {
			t.Fatal("expected send to block")
		}

		// Close the channel
		closeTask := &Task{}
		closeOp := &ChanOperation{
			opType: chanOpClose,
			ch:     ch,
		}
		tasks = cc.PushOperation(closeTask, closeOp)
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
		tasks = cc.PushOperation(recvTask, recvOp)
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
		tasks := cc.PushOperation(sendTask, sendOp)
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
		tasks = cc.PushOperation(recvTask, recvOp)
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
		tasks := cc.PushOperation(sendTask1, sendOp1)
		if len(tasks) != 0 {
			t.Fatal("expected first sender to block")
		}
		tasks = cc.PushOperation(sendTask2, sendOp2)
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
		tasks = cc.PushOperation(recvTask, recvOp)
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
		tasks := cc.PushOperation(recvTask, recvOp)
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
		tasks = cc.PushOperation(closeTask, closeOp)
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

		tasks = cc.PushOperation(newRecvTask, newRecvOp)
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
			tasks := cc.PushOperation(recvTask, recvOp)
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

		tasks := cc.PushOperation(closeTask, closeOp)
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

func TestChannelBufferedClose(t *testing.T) {
	t.Run("close buffered channel with receivers", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(2) // buffered channel with capacity 2

		// Add two messages to buffer
		ch.send(lua.LString("msg1"))
		ch.send(lua.LString("msg2"))

		// First receiver should get msg1 immediately
		recvTask1 := &Task{}
		recvOp1 := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}
		tasks := cc.PushOperation(recvTask1, recvOp1)
		if len(tasks) != 1 {
			t.Fatal("first receiver should get message immediately")
		}
		if recvTask1.resumeVal.String() != "msg1" {
			t.Errorf("first receiver expected msg1, got %v", recvTask1.resumeVal)
		}

		// Second receiver should get msg2 immediately
		recvTask2 := &Task{}
		recvOp2 := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}
		tasks = cc.PushOperation(recvTask2, recvOp2)
		if len(tasks) != 1 {
			t.Fatal("second receiver should get message immediately")
		}
		if recvTask2.resumeVal.String() != "msg2" {
			t.Errorf("second receiver expected msg2, got %v", recvTask2.resumeVal)
		}

		// Third receiver should block as buffer is empty
		recvTask3 := &Task{}
		recvOp3 := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}
		tasks = cc.PushOperation(recvTask3, recvOp3)
		if len(tasks) != 0 {
			t.Fatal("third receiver should block")
		}

		// Close the channel
		closeTask := &Task{}
		closeOp := &ChanOperation{
			opType: chanOpClose,
			ch:     ch,
		}
		tasks = cc.PushOperation(closeTask, closeOp)
		if len(tasks) != 2 { // close task + blocked receiver
			t.Fatalf("expected 2 tasks to resume, got %d", len(tasks))
		}

		// Verify third receiver got closed signal
		if recvTask3.resumeVal != lua.LNil {
			t.Errorf("third receiver expected nil (closed), got %v", recvTask3.resumeVal)
		}

		// Verify buffer was cleaned up after all messages read
		if !ch.isEmpty() {
			t.Error("expected buffer to be empty after all messages read")
		}
	})

	t.Run("receive after close with buffered messages", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(1)

		// Add message to buffer
		ch.send(lua.LString("buffered"))

		// Close channel
		closeTask := &Task{}
		closeOp := &ChanOperation{
			opType: chanOpClose,
			ch:     ch,
		}
		cc.PushOperation(closeTask, closeOp)

		// Try to receive after close
		recvTask := &Task{}
		recvOp := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}

		tasks := cc.PushOperation(recvTask, recvOp)
		if len(tasks) != 1 {
			t.Fatal("expected receive to complete immediately")
		}

		// Should get buffered message
		if recvTask.resumeVal.String() != "buffered" {
			t.Errorf("expected buffered message, got %v", recvTask.resumeVal)
		}

		// Second receive should get closed signal
		recvTask2 := &Task{}
		recvOp2 := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}

		tasks = cc.PushOperation(recvTask2, recvOp2)
		if len(tasks) != 1 {
			t.Fatal("expected receive to complete immediately")
		}

		if recvTask2.resumeVal != lua.LNil {
			t.Errorf("expected nil (closed), got %v", recvTask2.resumeVal)
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
			cc.PushOperation(recvTask, recvOp)
			cc.PushOperation(sendTask, sendOp)
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
			cc.PushOperation(sendTask, sendOp)
			cc.PushOperation(recvTask, recvOp)
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
				cc.PushOperation(sendTask, sendOp)

				recvTask := &Task{}
				recvOp := &ChanOperation{
					opType: chanOpReceive,
					ch:     ch,
				}
				cc.PushOperation(recvTask, recvOp)
			}

			// Close channel with pending operations
			closeTask := &Task{}
			closeOp := &ChanOperation{
				opType: chanOpClose,
				ch:     ch,
			}
			cc.PushOperation(closeTask, closeOp)
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
				cc.PushOperation(sendTask, sendOp)

			case 2: // Receive
				recvTask := &Task{}
				recvOp := &ChanOperation{
					opType: chanOpReceive,
					ch:     ch,
				}
				cc.PushOperation(recvTask, recvOp)

			case 3: // Close and create new channel
				closeTask := &Task{}
				closeOp := &ChanOperation{
					opType: chanOpClose,
					ch:     ch,
				}
				cc.PushOperation(closeTask, closeOp)
				ch = newLuaChannel(5)
			}
		}
	})
}

func TestChannelCoordinator_MultipleReceiversClose(t *testing.T) {
	t.Run("multiple receivers get closed signal", func(t *testing.T) {
		cc := NewChannelCoordinator()
		ch := newLuaChannel(0)

		// Add two receivers first
		recvTask1 := &Task{}
		recvOp1 := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}
		tasks := cc.PushOperation(recvTask1, recvOp1)
		if len(tasks) != 0 {
			t.Fatal("expected first receiver to block")
		}

		recvTask2 := &Task{}
		recvOp2 := &ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		}
		tasks = cc.PushOperation(recvTask2, recvOp2)
		if len(tasks) != 0 {
			t.Fatal("expected second receiver to block")
		}

		// Close channel and verify both receivers get notified
		closeTask := &Task{}
		closeOp := &ChanOperation{
			opType: chanOpClose,
			ch:     ch,
		}
		tasks = cc.PushOperation(closeTask, closeOp)
		if len(tasks) != 3 { // close task + 2 blocked receivers
			t.Fatalf("expected 3 tasks to resume, got %d", len(tasks))
		}

		// Verify both receivers got closed signal
		var closedCount int
		for _, task := range tasks {
			if task == recvTask1 || task == recvTask2 {
				if task.resumeVal != lua.LNil {
					t.Errorf("expected receiver to get nil (closed), got %v", task.resumeVal)
				}
				closedCount++
			}
		}

		if closedCount != 2 {
			t.Error("not all receivers got closed signal")
		}

		// Verify queues are cleaned up
		if cc.receivers[ch] != nil {
			t.Error("expected receiver queue to be empty")
		}
	})
}
