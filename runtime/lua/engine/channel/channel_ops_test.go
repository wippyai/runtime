package channel

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestChannelOps(t *testing.T) {
	t.Run("immediate operations on buffered channel", func(t *testing.T) {
		ops := newChannelOps()
		ch := newChannel(2)

		// Test immediate send
		ok := ops.trySend(ch, lua.LString("test1"))
		assert.True(t, ok, "should send to buffer")
		assert.Equal(t, 1, ch.size)

		// Test immediate receive
		value, ok := ops.tryReceive(ch)
		assert.True(t, ok, "should receive from buffer")
		assert.Equal(t, lua.LString("test1"), value)
		assert.Equal(t, 0, ch.size)
	})

	t.Run("queued operations", func(t *testing.T) {
		ops := newChannelOps()
		ch := newChannel(0) // unbuffered

		// Queue a receiver
		recvTask := &engine.Task{}
		recvOp := &chanOperation{
			dir: chanReceive,
			ch:  ch,
		}
		ops.queueOperation(recvTask, recvOp, nil)

		// Try to handle a send operation
		task := ops.findReceiver(ch, lua.LString("test"))
		assert.NotNil(t, task)
		assert.Equal(t, recvTask, task)
		assert.Equal(t, lua.LString("test"), task.Resumed[0])
		assert.Equal(t, lua.LBool(true), task.Resumed[1])
	})

	t.Run("FIFO ordering", func(t *testing.T) {
		ops := newChannelOps()
		ch := newChannel(0)

		// Queue multiple receivers
		var tasks []*engine.Task
		for i := 0; i < 3; i++ {
			task := &engine.Task{}
			op := &chanOperation{
				dir: chanReceive,
				ch:  ch,
			}
			ops.queueOperation(task, op, nil)
			tasks = append(tasks, task)
		}

		// Handle them in order
		values := []string{"first", "second", "third"}
		for i, val := range values {
			task := ops.findReceiver(ch, lua.LString(val))
			assert.Equal(t, tasks[i], task, "receivers should be handled in FIFO order")
			assert.Equal(t, lua.LString(val), task.Resumed[0])
		}
	})

	t.Run("select operations", func(t *testing.T) {
		ops := newChannelOps()
		ch := newChannel(0)
		selectOp := &selectOperation{}

		// Queue a receiver with select
		recvTask := &engine.Task{}
		recvOp := &chanOperation{
			dir: chanReceive,
			ch:  ch,
		}
		ops.queueOperation(recvTask, recvOp, selectOp)

		// Handle the select case
		task := ops.findReceiver(ch, lua.LString("test"))
		assert.NotNil(t, task)
		assert.Equal(t, recvTask, task)
		assert.Len(t, task.Resumed, 1) // Select result table
	})

	t.Run("closed channel behavior", func(t *testing.T) {
		ops := newChannelOps()
		ch := newChannel(1)
		ch.closed = true

		// Try send on closed channel
		ok := ops.trySend(ch, lua.LString("test"))
		assert.False(t, ok, "send on closed channel should fail")

		// Try receive on closed empty channel
		value, ok := ops.tryReceive(ch)
		assert.False(t, ok, "receive on closed empty channel should return false")
		assert.Nil(t, value)

		// Try receive on closed channel with buffered value
		ch = newChannel(1)
		ch.send(lua.LString("buffered"))
		ch.closed = true
		value, ok = ops.tryReceive(ch)
		assert.True(t, ok, "should receive buffered value from closed channel")
		assert.Equal(t, lua.LString("buffered"), value)
	})

	t.Run("resource cleanup", func(t *testing.T) {
		ops := newChannelOps()
		ch := newChannel(0)

		// Queue some operations
		for i := 0; i < 3; i++ {
			task := &engine.Task{}
			op := &chanOperation{
				dir: chanReceive,
				ch:  ch,
			}
			ops.queueOperation(task, op, nil)
		}

		ops.cleanup()

		// Verify queues are cleared
		assert.Zero(t, ops.receivers.getQueueSize(ch))
		assert.Zero(t, ops.senders.getQueueSize(ch))
	})

	t.Run("sender queue operations", func(t *testing.T) {
		ops := newChannelOps()
		ch := newChannel(0)

		// Queue a sender
		sendTask := &engine.Task{}
		sendOp := &chanOperation{
			dir:   chanSend,
			ch:    ch,
			value: lua.LString("test"),
		}
		ops.queueOperation(sendTask, sendOp, nil)

		// Handle sender queue
		value, task := ops.findSender(ch)
		assert.NotNil(t, task)
		assert.Equal(t, sendTask, task)
		assert.Equal(t, lua.LString("test"), value)
		assert.Equal(t, lua.LBool(true), task.Resumed[0])

		// Queue should be empty
		assert.Zero(t, ops.senders.getQueueSize(ch))
	})

	t.Run("mixed operations", func(t *testing.T) {
		ops := newChannelOps()
		ch := newChannel(1)

		// Fill buffer
		ok := ops.trySend(ch, lua.LString("buffered"))
		assert.True(t, ok)

		// Queue a sender (should block)
		sendTask := &engine.Task{}
		sendOp := &chanOperation{
			dir:   chanSend,
			ch:    ch,
			value: lua.LString("queued"),
		}
		ops.queueOperation(sendTask, sendOp, nil)

		// Receive should get buffered value first
		value, ok := ops.tryReceive(ch)
		assert.True(t, ok)
		assert.Equal(t, lua.LString("buffered"), value)

		// Now queued send should proceed
		value, task := ops.findSender(ch)
		assert.NotNil(t, task)
		assert.Equal(t, lua.LString("queued"), value)
	})
}
