package channel

/**
package channel

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

type mockVM struct {
	stepFunc func(tasks ...*engine.Task) ([]*engine.Task, error)
}

func (m *mockVM) Step(tasks ...*engine.Task) ([]*engine.Task, error) {
	return m.stepFunc(tasks...)
}

func TestSchedulerDirect(t *testing.T) {
	t.Run("send_receive_unbuffered", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(0)

		// Create tasks
		sendTask := &engine.Task{}
		sendOp := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("test"),
		}

		recvTask := &engine.Task{}
		recvOp := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}

		// Try receive first
		tasks := s.pushOperation(recvTask, recvOp)
		assert.Empty(t, tasks, "receiver should block")
		assert.NotNil(t, s.receivers.queues[ch], "receiver should be queued")

		// Then send
		tasks = s.pushOperation(sendTask, sendOp)
		assert.Len(t, tasks, 2, "should resume both tasks")

		// Verify send succeeded
		assert.Equal(t, lua.LBool(true), sendTask.Resumed[0], "send should succeed")

		// Verify receive got value
		assert.Equal(t, lua.LString("test"), recvTask.Resumed[0], "receive should get value")

		// Verify queues are empty
		assert.Nil(t, s.receivers.queues[ch], "receiver queue should be empty")
		assert.Nil(t, s.senders.queues[ch], "sender queue should be empty")
	})

	t.Run("buffered_channel_operations", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(2)

		// Send first message
		send1 := &engine.Task{}
		sendOp1 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("msg1"),
		}
		tasks := s.pushOperation(send1, sendOp1)
		assert.Len(t, tasks, 1, "first send should complete")
		assert.Equal(t, lua.LBool(true), send1.Resumed[0])

		// Send second message
		send2 := &engine.Task{}
		sendOp2 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("msg2"),
		}
		tasks = s.pushOperation(send2, sendOp2)
		assert.Len(t, tasks, 1, "second send should complete")

		// Third send should block
		send3 := &engine.Task{}
		sendOp3 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("msg3"),
		}
		tasks = s.pushOperation(send3, sendOp3)
		assert.Empty(t, tasks, "third send should block")
		assert.NotNil(t, s.senders.queues[ch], "sender should be queued")

		// First receive completes immediately with buffered value
		recv1 := &engine.Task{}
		recvOp1 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks = s.pushOperation(recv1, recvOp1)
		assert.Len(t, tasks, 1, "first receive should complete")
		assert.Equal(t, lua.LString("msg1"), recv1.Resumed[0])

		// Second receive also completes immediately with second buffered value
		recv2 := &engine.Task{}
		recvOp2 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks = s.pushOperation(recv2, recvOp2)
		assert.Len(t, tasks, 1, "second receive should complete")
		assert.Equal(t, lua.LString("msg2"), recv2.Resumed[0])
	})

	t.Run("close_with_pending_operations", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(1)

		// Send first message to buffer
		send1 := &engine.Task{}
		sendOp1 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("msg1"),
		}
		tasks := s.pushOperation(send1, sendOp1)
		assert.Len(t, tasks, 1, "first send should complete")

		// Second send blocks
		send2 := &engine.Task{}
		sendOp2 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("msg2"),
		}
		tasks = s.pushOperation(send2, sendOp2)
		assert.Empty(t, tasks, "second send should block")

		// First receive gets buffered value
		recv1 := &engine.Task{}
		recvOp1 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks = s.pushOperation(recv1, recvOp1)
		assert.Len(t, tasks, 1, "first receive should complete")
		assert.Equal(t, lua.LString("msg1"), recv1.Resumed[0])

		// Close channel
		closeTask := &engine.Task{}
		closeOp := &chanOperation{
			opType: chanClose,
			ch:     ch,
		}
		tasks = s.pushOperation(closeTask, closeOp)

		// Close task and blocked sender should be resumed
		assert.Len(t, tasks, 2, "close should resume pending operations")

		// Verify sender got nil (channel closed)
		assert.Equal(t, lua.LNil, send2.Resumed[0], "sender should get nil on closed channel")

		// Verify subsequent receive gets nil
		recv2 := &engine.Task{}
		recvOp2 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks = s.pushOperation(recv2, recvOp2)
		assert.Len(t, tasks, 1, "receive on closed channel should complete immediately")
		assert.Equal(t, lua.LNil, recv2.Resumed[0], "receive should get nil from closed channel")

		// Verify queues are cleaned up
		assert.Nil(t, s.senders.queues[ch], "sender queue should be empty")
		assert.Nil(t, s.receivers.queues[ch], "receiver queue should be empty")
	})

	t.Run("object_pool_reuse", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(0)

		// Create and queue multiple operations
		for i := 0; i < 10; i++ {
			sendTask := &engine.Task{}
			sendOp := &chanOperation{
				opType: chanSend,
				ch:     ch,
				value:  lua.LNumber(i),
			}
			s.pushOperation(sendTask, sendOp)

			recvTask := &engine.Task{}
			recvOp := &chanOperation{
				opType: chanReceive,
				ch:     ch,
			}
			tasks := s.pushOperation(recvTask, recvOp)

			// Each pair should complete together
			assert.Len(t, tasks, 2, "send/receive pair should complete")
		}

		// Verify no leaks in queues
		assert.Nil(t, s.senders.queues[ch], "sender queue should be empty")
		assert.Nil(t, s.receivers.queues[ch], "receiver queue should be empty")
	})

	t.Run("multiple_receivers_waiting", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(0)

		// Queue two receivers first
		recv1 := &engine.Task{}
		recvOp1 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks := s.pushOperation(recv1, recvOp1)
		assert.Empty(t, tasks, "first receiver should block")

		recv2 := &engine.Task{}
		recvOp2 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks = s.pushOperation(recv2, recvOp2)
		assert.Empty(t, tasks, "second receiver should block")

		// Send first value - should wake up first receiver
		send1 := &engine.Task{}
		sendOp1 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("first"),
		}
		tasks = s.pushOperation(send1, sendOp1)
		assert.Len(t, tasks, 2, "first send should wake first receiver")
		assert.Equal(t, lua.LString("first"), recv1.Resumed[0], "first receiver should get first value")

		// Send second value - should wake up second receiver
		send2 := &engine.Task{}
		sendOp2 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("second"),
		}
		tasks = s.pushOperation(send2, sendOp2)
		assert.Len(t, tasks, 2, "second send should wake second receiver")
		assert.Equal(t, lua.LString("second"), recv2.Resumed[0], "second receiver should get second value")
	})

	t.Run("multiple_senders_waiting", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(0)

		// Queue two senders first
		send1 := &engine.Task{}
		sendOp1 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("first"),
		}
		tasks := s.pushOperation(send1, sendOp1)
		assert.Empty(t, tasks, "first sender should block")

		send2 := &engine.Task{}
		sendOp2 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("second"),
		}
		tasks = s.pushOperation(send2, sendOp2)
		assert.Empty(t, tasks, "second sender should block")

		// Receive first - should wake up first sender
		recv1 := &engine.Task{}
		recvOp1 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks = s.pushOperation(recv1, recvOp1)
		assert.Len(t, tasks, 2, "first receive should wake first sender")
		assert.Equal(t, lua.LString("first"), recv1.Resumed[0], "should receive first value")

		// Receive second - should wake up second sender
		recv2 := &engine.Task{}
		recvOp2 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks = s.pushOperation(recv2, recvOp2)
		assert.Len(t, tasks, 2, "second receive should wake second sender")
		assert.Equal(t, lua.LString("second"), recv2.Resumed[0], "should receive second value")
	})

	t.Run("close_with_multiple_receivers", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(0)

		// Queue multiple receivers
		var receivers []*engine.Task
		for i := 0; i < 3; i++ {
			task := &engine.Task{}
			op := &chanOperation{
				opType: chanReceive,
				ch:     ch,
			}
			tasks := s.pushOperation(task, op)
			assert.Empty(t, tasks, "receiver should block")
			receivers = append(receivers, task)
		}

		// Close the channel
		closeTask := &engine.Task{}
		closeOp := &chanOperation{
			opType: chanClose,
			ch:     ch,
		}
		tasks := s.pushOperation(closeTask, closeOp)
		assert.Len(t, tasks, 4, "close should resume all receivers + close task")

		// Verify all receivers got nil
		for i, recv := range receivers {
			assert.Equal(t, lua.LNil, recv.Resumed[0], "receiver %d should get nil on closed channel", i)
		}
	})

	t.Run("close_buffered_with_receivers", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(2)

		// Fill the buffer
		send1 := &engine.Task{}
		sendOp1 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("msg1"),
		}
		tasks := s.pushOperation(send1, sendOp1)
		assert.Len(t, tasks, 1, "first send should complete")

		send2 := &engine.Task{}
		sendOp2 := &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("msg2"),
		}
		tasks = s.pushOperation(send2, sendOp2)
		assert.Len(t, tasks, 1, "second send should complete")

		// Close the channel
		closeTask := &engine.Task{}
		closeOp := &chanOperation{
			opType: chanClose,
			ch:     ch,
		}
		tasks = s.pushOperation(closeTask, closeOp)
		assert.Len(t, tasks, 1, "close task should complete")

		// First receive should get first buffered message
		recv1 := &engine.Task{}
		recvOp1 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks = s.pushOperation(recv1, recvOp1)
		assert.Len(t, tasks, 1, "first receive should complete")
		assert.Equal(t, lua.LString("msg1"), recv1.Resumed[0], "should get first buffered message")

		// Second receive should get second buffered message
		recv2 := &engine.Task{}
		recvOp2 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks = s.pushOperation(recv2, recvOp2)
		assert.Len(t, tasks, 1, "second receive should complete")
		assert.Equal(t, lua.LString("msg2"), recv2.Resumed[0], "should get second buffered message")

		// Third receive should get nil (channel closed and empty)
		recv3 := &engine.Task{}
		recvOp3 := &chanOperation{
			opType: chanReceive,
			ch:     ch,
		}
		tasks = s.pushOperation(recv3, recvOp3)
		assert.Len(t, tasks, 1, "receive on closed empty channel should complete")
		assert.Equal(t, lua.LNil, recv3.Resumed[0], "should get nil from closed empty channel")
	})

	t.Run("buffered_channel_send_receive_order", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(3)

		// Send three messages
		values := []string{"first", "second", "third"}
		var sends []*engine.Task

		for _, val := range values {
			task := &engine.Task{}
			op := &chanOperation{
				opType: chanSend,
				ch:     ch,
				value:  lua.LString(val),
			}
			tasks := s.pushOperation(task, op)
			assert.Len(t, tasks, 1, "send to non-full buffer should complete")
			sends = append(sends, task)
		}

		// Receive them in order
		for i, expected := range values {
			task := &engine.Task{}
			op := &chanOperation{
				opType: chanReceive,
				ch:     ch,
			}
			tasks := s.pushOperation(task, op)
			assert.Len(t, tasks, 1, "receive from non-empty buffer should complete")
			assert.Equal(t, lua.LString(expected), task.Resumed[0],
				"messages should be received in order %d", i)
		}
	})
}

func TestScheduler_Step(t *testing.T) {
	t.Run("basic channel operations", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(0)

		senderTask := &engine.Task{}
		receiverTask := &engine.Task{}
		otherTask := &engine.Task{}

		firstCall := true
		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				if firstCall {
					firstCall = false

					senderTask.Yielded = []lua.LValue{&chanOperation{
						opType: chanSend,
						ch:     ch,
						value:  lua.LString("test"),
					}}

					receiverTask.Yielded = []lua.LValue{&chanOperation{
						opType: chanReceive,
						ch:     ch,
					}}

					otherTask.Yielded = []lua.LValue{lua.LNil}
					return []*engine.Task{senderTask, otherTask, receiverTask}, nil
				}

				// no more tasks to run
				return nil, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)

		// check we got other task
		assert.Equal(t, otherTask, tasks[0])
	})
}

*/
