package channel

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

// Mock VM for testing
type mockVM struct {
	tasks []*engine.Task
}

func (m *mockVM) Step(tasks ...*engine.Task) ([]*engine.Task, error) {
	m.tasks = append(m.tasks, tasks...)
	return tasks, nil
}

func TestSchedulerResources(t *testing.T) {
	t.Run("resource lifecycle", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(2)

		// Test buffer allocation
		assert.NotNil(t, ch.buffer, "channel should have allocated buffer")
		assert.Equal(t, 2, len(ch.buffer), "buffer should have correct capacity")

		// Test proper cleanup after channel operations
		senderTask := &engine.Task{}
		sendOp := &chanOperation{
			dir:   chanSend,
			ch:    ch,
			value: lua.LString("test message"),
		}
		senderTask.Yielded = []lua.LValue{sendOp}

		tasks, err := scheduler.handleTasks([]*engine.Task{senderTask})
		assert.NoError(t, err)
		assert.Len(t, tasks, 1, "send should complete")

		// Verify message is in buffer
		assert.Equal(t, 1, ch.size)
		assert.NotNil(t, ch.buffer[0])

		// Test receive cleans up buffer slot
		receiverTask := &engine.Task{}
		recvOp := &chanOperation{
			dir: chanReceive,
			ch:  ch,
		}
		receiverTask.Yielded = []lua.LValue{recvOp}

		tasks, err = scheduler.handleTasks([]*engine.Task{receiverTask})
		assert.NoError(t, err)
		assert.Len(t, tasks, 1, "receive should complete")
		assert.Nil(t, ch.buffer[0], "buffer slot should be cleared after receive")
		assert.Equal(t, 0, ch.size)

		// Cleanup should release all resources
		ch.cleanup()
		assert.Nil(t, ch.buffer)
		assert.Equal(t, 0, ch.size)
		assert.Equal(t, 0, ch.read)
		assert.Equal(t, 0, ch.write)
	})

	t.Run("queue resource management", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(0)

		// Queue multiple receivers
		var receiverTasks []*engine.Task
		for i := 0; i < 3; i++ {
			task := &engine.Task{}
			recvOp := &chanOperation{
				dir: chanReceive,
				ch:  ch,
			}
			task.Yielded = []lua.LValue{recvOp}
			receiverTasks = append(receiverTasks, task)
		}

		// Process receivers - they should all block
		tasks, err := scheduler.handleTasks(receiverTasks)
		assert.NoError(t, err)
		assert.Empty(t, tasks, "receivers should block")
		assert.Equal(t, 3, scheduler.receivers.getQueueSize(ch))

		// Send messages one by one
		for i := 0; i < 3; i++ {
			senderTask := &engine.Task{}
			sendOp := &chanOperation{
				dir:   chanSend,
				ch:    ch,
				value: lua.LString("message"),
			}
			senderTask.Yielded = []lua.LValue{sendOp}

			tasks, err := scheduler.handleTasks([]*engine.Task{senderTask})
			assert.NoError(t, err)
			assert.Len(t, tasks, 2, "send should complete with receiver")
			assert.Equal(t, 2-i, scheduler.receivers.getQueueSize(ch))
		}

		// Verify queues are cleaned up
		assert.Nil(t, scheduler.receivers.queues[ch])
	})

	t.Run("buffer cycling and cleanup", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(2)

		// Fill buffer multiple times to test cycling
		for cycle := 0; cycle < 3; cycle++ {
			// Fill buffer
			for i := 0; i < 2; i++ {
				senderTask := &engine.Task{}
				sendOp := &chanOperation{
					dir:   chanSend,
					ch:    ch,
					value: lua.LString("test"),
				}
				senderTask.Yielded = []lua.LValue{sendOp}

				tasks, err := scheduler.handleTasks([]*engine.Task{senderTask})
				assert.NoError(t, err)
				assert.Len(t, tasks, 1)
			}

			// Empty buffer
			for i := 0; i < 2; i++ {
				receiverTask := &engine.Task{}
				recvOp := &chanOperation{
					dir: chanReceive,
					ch:  ch,
				}
				receiverTask.Yielded = []lua.LValue{recvOp}

				tasks, err := scheduler.handleTasks([]*engine.Task{receiverTask})
				assert.NoError(t, err)
				assert.Len(t, tasks, 1)
			}

			// Verify buffer state after cycle
			assert.Equal(t, 0, ch.size)
			for i := range ch.buffer {
				assert.Nil(t, ch.buffer[i], "buffer slots should be cleaned up")
			}
		}
	})

	t.Run("pending operation cleanup", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(1)

		// Queue operations that will be pending
		senderTask1 := &engine.Task{}
		senderTask1.Yielded = []lua.LValue{&chanOperation{
			dir:   chanSend,
			ch:    ch,
			value: lua.LString("msg1"),
		}}

		senderTask2 := &engine.Task{}
		senderTask2.Yielded = []lua.LValue{&chanOperation{
			dir:   chanSend,
			ch:    ch,
			value: lua.LString("msg2"),
		}}

		// First send succeeds, second blocks
		tasks, err := scheduler.handleTasks([]*engine.Task{senderTask1, senderTask2})
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, 1, scheduler.senders.getQueueSize(ch))

		// Close channel
		closeTask := &engine.Task{}
		closeTask.Yielded = []lua.LValue{&chanOperation{
			dir: chanClose,
			ch:  ch,
		}}

		tasks, err = scheduler.handleTasks([]*engine.Task{closeTask})
		assert.NoError(t, err)
		assert.Contains(t, tasks, closeTask)
		assert.Contains(t, tasks, senderTask2)

		// Verify cleanup
		assert.Nil(t, scheduler.senders.queues[ch])
		assert.Nil(t, scheduler.receivers.queues[ch])
	})
}

func TestScheduler(t *testing.T) {
	t.Run("unbuffered channel operations", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(0)

		// Create sender and receiver tasks
		senderTask := &engine.Task{}
		sendOp := &chanOperation{
			dir:   chanSend,
			ch:    ch,
			value: lua.LString("test message"),
		}
		senderTask.Yielded = []lua.LValue{sendOp}

		receiverTask := &engine.Task{}
		recvOp := &chanOperation{
			dir: chanReceive,
			ch:  ch,
		}
		receiverTask.Yielded = []lua.LValue{recvOp}

		// Process receiver first - should block
		tasks, err := scheduler.handleTasks([]*engine.Task{receiverTask})
		assert.NoError(t, err)
		assert.Empty(t, tasks, "receiver should block")
		assert.Equal(t, 1, scheduler.receivers.getQueueSize(ch))

		// Process sender - should complete both operations
		tasks, err = scheduler.handleTasks([]*engine.Task{senderTask})
		assert.NoError(t, err)
		assert.Len(t, tasks, 2, "both tasks should complete")
		assert.Equal(t, lua.LBool(true), senderTask.Resumed[0], "send should succeed")
		assert.Equal(t, lua.LString("test message"), receiverTask.Resumed[0], "receiver should get message")
	})

	t.Run("buffered channel operations", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(2)

		// Test sending to buffer
		senderTask := &engine.Task{}
		sendOp := &chanOperation{
			dir:   chanSend,
			ch:    ch,
			value: lua.LString("buffered message"),
		}
		senderTask.Yielded = []lua.LValue{sendOp}

		tasks, err := scheduler.handleTasks([]*engine.Task{senderTask})
		assert.NoError(t, err)
		assert.Len(t, tasks, 1, "send to buffer should complete immediately")
		assert.Equal(t, lua.LBool(true), senderTask.Resumed[0])

		// Test receiving from buffer
		receiverTask := &engine.Task{}
		recvOp := &chanOperation{
			dir: chanReceive,
			ch:  ch,
		}
		receiverTask.Yielded = []lua.LValue{recvOp}

		tasks, err = scheduler.handleTasks([]*engine.Task{receiverTask})
		assert.NoError(t, err)
		assert.Len(t, tasks, 1, "receive from buffer should complete immediately")
		assert.Equal(t, lua.LString("buffered message"), receiverTask.Resumed[0])
	})

	t.Run("channel close operations", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(1)

		// Queue a receiver
		receiverTask := &engine.Task{}
		recvOp := &chanOperation{
			dir: chanReceive,
			ch:  ch,
		}
		receiverTask.Yielded = []lua.LValue{recvOp}

		tasks, err := scheduler.handleTasks([]*engine.Task{receiverTask})
		assert.NoError(t, err)
		assert.Empty(t, tasks, "receiver should block")

		// Close the channel
		closeTask := &engine.Task{}
		closeOp := &chanOperation{
			dir: chanClose,
			ch:  ch,
		}
		closeTask.Yielded = []lua.LValue{closeOp}

		tasks, err = scheduler.handleTasks([]*engine.Task{closeTask})
		assert.NoError(t, err)
		assert.Len(t, tasks, 2, "close should unblock receiver")
		assert.Equal(t, lua.LBool(true), closeTask.Resumed[0], "close should succeed")
		assert.Equal(t, lua.LNil, receiverTask.Resumed[0], "receiver should get nil")
		assert.Equal(t, lua.LBool(false), receiverTask.Resumed[1], "receiver should get false ok value")
	})

	t.Run("select operations", func(t *testing.T) {
		scheduler := newScheduler()
		ch1 := newChannel(0)
		ch2 := newChannel(0)

		// Create select operation with two cases
		selectTask := &engine.Task{}
		selectOp := &selectOperation{
			cases: []*selectCase{
				{
					chValue: &lua.LUserData{Value: ch1},
					dir:     chanReceive,
				},
				{
					chValue: &lua.LUserData{Value: ch2},
					dir:     chanReceive,
				},
			},
		}
		selectTask.Yielded = []lua.LValue{selectOp}

		// Process select - should block
		tasks, err := scheduler.handleTasks([]*engine.Task{selectTask})
		assert.NoError(t, err)
		assert.Empty(t, tasks, "select should block")

		// Send to first channel
		senderTask := &engine.Task{}
		sendOp := &chanOperation{
			dir:   chanSend,
			ch:    ch1,
			value: lua.LString("selected message"),
		}
		senderTask.Yielded = []lua.LValue{sendOp}

		tasks, err = scheduler.handleTasks([]*engine.Task{senderTask})
		assert.NoError(t, err)
		assert.Len(t, tasks, 2, "send should complete select")

		// Verify select result
		result, ok := selectTask.Resumed[0].(*lua.LTable)
		assert.True(t, ok, "select should return table")
		assert.Equal(t, ch1, result.RawGetString("channel").(*lua.LUserData).Value)
		assert.Equal(t, lua.LString("selected message"), result.RawGetString("value"))
		assert.Equal(t, lua.LBool(true), result.RawGetString("ok"))
	})

	t.Run("named channel operations", func(t *testing.T) {
		scheduler := newScheduler()
		ch := Named("test-inbox", 0)

		// Queue a receiver
		receiverTask := &engine.Task{}
		recvOp := &chanOperation{
			dir: chanReceive,
			ch:  ch,
		}
		receiverTask.Yielded = []lua.LValue{recvOp}

		tasks, err := scheduler.handleTasks([]*engine.Task{receiverTask})
		assert.NoError(t, err)
		assert.Empty(t, tasks, "receiver should block")

		// Send to named channel
		tasks, err = scheduler.send("test-inbox", lua.LString("inbox message"))
		assert.NoError(t, err)
		assert.Len(t, tasks, 1, "send should unblock receiver")
		assert.Equal(t, lua.LString("inbox message"), receiverTask.Resumed[0])
		assert.Equal(t, lua.LBool(true), receiverTask.Resumed[1])
	})

	t.Run("cleanup", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(1)

		// Queue some operations
		senderTask := &engine.Task{}
		sendOp := &chanOperation{
			dir:   chanSend,
			ch:    ch,
			value: lua.LString("test"),
		}
		senderTask.Yielded = []lua.LValue{sendOp}

		receiverTask := &engine.Task{}
		recvOp := &chanOperation{
			dir: chanReceive,
			ch:  ch,
		}
		receiverTask.Yielded = []lua.LValue{recvOp}

		scheduler.handleTasks([]*engine.Task{senderTask, receiverTask})

		// Cleanup
		scheduler.Cleanup()

		assert.Nil(t, scheduler.senders.queues[ch])
		assert.Nil(t, scheduler.receivers.queues[ch])
	})
}

func TestScheduler_Error_Cases(t *testing.T) {
	t.Run("send to closed channel", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(0)
		ch.closed = true

		senderTask := &engine.Task{}
		sendOp := &chanOperation{
			dir:   chanSend,
			ch:    ch,
			value: lua.LString("test"),
		}
		senderTask.Yielded = []lua.LValue{sendOp}

		tasks, err := scheduler.handleTasks([]*engine.Task{senderTask})
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, lua.LNil, senderTask.Resumed[0])
	})

	t.Run("receive from closed empty channel", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(0)
		ch.closed = true

		receiverTask := &engine.Task{}
		recvOp := &chanOperation{
			dir: chanReceive,
			ch:  ch,
		}
		receiverTask.Yielded = []lua.LValue{recvOp}

		tasks, err := scheduler.handleTasks([]*engine.Task{receiverTask})
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, lua.LNil, receiverTask.Resumed[0])
		assert.Equal(t, lua.LBool(false), receiverTask.Resumed[1])
	})

	t.Run("send to non-existent named channel", func(t *testing.T) {
		scheduler := newScheduler()

		_, err := scheduler.send("non-existent", lua.LString("test"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no receiver found")
	})

	t.Run("queue removal conditions", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(2) // Buffered channel

		// 1. First verify channel state with buffered values

		// Fill buffer
		for i := 0; i < 2; i++ {
			senderTask := &engine.Task{}
			senderTask.Yielded = []lua.LValue{&chanOperation{
				dir:   chanSend,
				ch:    ch,
				value: lua.LString("msg"),
			}}
			tasks, err := scheduler.handleTasks([]*engine.Task{senderTask})
			assert.NoError(t, err)
			assert.Len(t, tasks, 1)
		}

		// Close channel while buffer has values
		closeTask := &engine.Task{}
		closeTask.Yielded = []lua.LValue{&chanOperation{
			dir: chanClose,
			ch:  ch,
		}}
		tasks, err := scheduler.handleTasks([]*engine.Task{closeTask})
		assert.NoError(t, err)
		assert.True(t, ch.closed)

		// Try to receive and verify we still get values
		recvTask := &engine.Task{}
		recvTask.Yielded = []lua.LValue{&chanOperation{
			dir: chanReceive,
			ch:  ch,
		}}
		tasks, err = scheduler.handleTasks([]*engine.Task{recvTask})
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, lua.LString("msg"), recvTask.Resumed[0], "should get buffered value")
		assert.Equal(t, lua.LBool(true), recvTask.Resumed[1], "should indicate success")
		assert.True(t, ch.closed, "channel should remain closed")
		assert.Equal(t, 1, ch.size, "should still have one buffered value")

		// 2. Now verify non-closed channel behavior
		ch2 := newChannel(2)

		// Fill and drain buffer without closing
		senderTask := &engine.Task{}
		senderTask.Yielded = []lua.LValue{&chanOperation{
			dir:   chanSend,
			ch:    ch2,
			value: lua.LString("msg"),
		}}
		tasks, err = scheduler.handleTasks([]*engine.Task{senderTask})
		assert.NoError(t, err)

		recvTask = &engine.Task{}
		recvTask.Yielded = []lua.LValue{&chanOperation{
			dir: chanReceive,
			ch:  ch2,
		}}
		tasks, err = scheduler.handleTasks([]*engine.Task{recvTask})
		assert.NoError(t, err)

		// Verify channel state when drained but not closed
		assert.False(t, ch2.closed, "channel should remain open")
		assert.Equal(t, 0, ch2.size, "should be drained")

		// 3. Finally verify closed and drained state
		ch3 := newChannel(2)

		// Send a value
		senderTask = &engine.Task{}
		senderTask.Yielded = []lua.LValue{&chanOperation{
			dir:   chanSend,
			ch:    ch3,
			value: lua.LString("msg"),
		}}
		tasks, err = scheduler.handleTasks([]*engine.Task{senderTask})
		assert.NoError(t, err)

		// Close channel
		closeTask = &engine.Task{}
		closeTask.Yielded = []lua.LValue{&chanOperation{
			dir: chanClose,
			ch:  ch3,
		}}
		tasks, err = scheduler.handleTasks([]*engine.Task{closeTask})
		assert.NoError(t, err)

		// Drain the channel
		recvTask = &engine.Task{}
		recvTask.Yielded = []lua.LValue{&chanOperation{
			dir: chanReceive,
			ch:  ch3,
		}}
		tasks, err = scheduler.handleTasks([]*engine.Task{recvTask})
		assert.NoError(t, err)

		// Try one more receive which should give nil, false for closed empty channel
		recvTask = &engine.Task{}
		recvTask.Yielded = []lua.LValue{&chanOperation{
			dir: chanReceive,
			ch:  ch3,
		}}
		tasks, err = scheduler.handleTasks([]*engine.Task{recvTask})
		assert.NoError(t, err)
		assert.Equal(t, lua.LNil, recvTask.Resumed[0], "should get nil for drained closed channel")
		assert.Equal(t, lua.LBool(false), recvTask.Resumed[1], "should indicate channel closed")

		// Verify final state
		assert.True(t, ch3.closed, "channel should remain closed")
		assert.Equal(t, 0, ch3.size, "should be fully drained")
	})

	t.Run("close already closed channel", func(t *testing.T) {
		scheduler := newScheduler()
		ch := newChannel(0)

		// First close should succeed
		closeTask1 := &engine.Task{}
		closeOp1 := &chanOperation{
			dir: chanClose,
			ch:  ch,
		}
		closeTask1.Yielded = []lua.LValue{closeOp1}

		tasks, err := scheduler.handleTasks([]*engine.Task{closeTask1})
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, lua.LBool(true), closeTask1.Resumed[0])
		assert.True(t, ch.closed)

		// Second close should panic like in Go
		closeTask2 := &engine.Task{}
		closeOp2 := &chanOperation{
			dir: chanClose,
			ch:  ch,
		}
		closeTask2.Yielded = []lua.LValue{closeOp2}

		tasks, err = scheduler.handleTasks([]*engine.Task{closeTask2})
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.NotNil(t, closeTask2.RaiseError, "closing already closed channel should raise error")
		assert.Contains(t, closeTask2.RaiseError.Error(), "already closed")
	})
}
