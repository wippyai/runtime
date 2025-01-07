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

func TestScheduler_Step(t *testing.T) {
	t.Run("basic channel operations", func(t *testing.T) {
		s := NewScheduler()
		ch := newLuaChannel(0)

		// Mock VM that returns tasks with channel operations
		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				result := make([]*engine.Task, 2)

				// Create a sender task
				senderTask := &engine.Task{}
				senderTask.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanSend,
					ch:     ch,
					value:  lua.LString("test"),
				}})
				result[0] = senderTask

				// Create a receiver task
				receiverTask := &engine.Task{}
				receiverTask.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanReceive,
					ch:     ch,
				}})
				result[1] = receiverTask

				return result, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Len(t, tasks, 2)

		// Verify sender and receiver were matched
		assert.Equal(t, lua.LBool(true), tasks[0].GetResumeValues()[0], "sender should complete successfully")
		assert.Equal(t, lua.LString("test"), tasks[1].GetResumeValues()[0], "receiver should get sent value")
	})

	t.Run("buffered channel operations", func(t *testing.T) {
		s := NewScheduler()
		ch := newLuaChannel(1) // buffered capacity 1

		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				result := make([]*engine.Task, 2)

				// Sender task - should complete immediately due to buffer
				senderTask := &engine.Task{}
				senderTask.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanSend,
					ch:     ch,
					value:  lua.LString("buffered"),
				}})
				result[0] = senderTask

				// Second send - should block
				sender2Task := &engine.Task{}
				sender2Task.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanSend,
					ch:     ch,
					value:  lua.LString("blocked"),
				}})
				result[1] = sender2Task

				return result, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, lua.LBool(true), tasks[0].GetResumeValues()[0], "first send should complete")

		// Now add a receiver
		vm.stepFunc = func(tasks ...*engine.Task) ([]*engine.Task, error) {
			receiverTask := &engine.Task{}
			receiverTask.SetYieldedValues([]lua.LValue{&chanOperation{
				opType: chanReceive,
				ch:     ch,
			}})
			return []*engine.Task{receiverTask}, nil
		}

		tasks, err = s.Step(vm)
		assert.NoError(t, err)
		assert.Len(t, tasks, 2)
		assert.Equal(t, lua.LString("buffered"), tasks[0].GetResumeValues()[0], "receiver should get buffered value")
		assert.Equal(t, lua.LBool(true), tasks[1].GetResumeValues()[0], "blocked send should complete")
	})

	t.Run("channel close operations", func(t *testing.T) {
		s := NewScheduler()
		ch := newLuaChannel(1)

		// Setup initial state with pending operations
		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				result := make([]*engine.Task, 3)

				// Queue a sender
				senderTask := &engine.Task{}
				senderTask.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanSend,
					ch:     ch,
					value:  lua.LString("pending"),
				}})
				result[0] = senderTask

				// Queue a receiver
				receiverTask := &engine.Task{}
				receiverTask.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanReceive,
					ch:     ch,
				}})
				result[1] = receiverTask

				// Close the channel
				closeTask := &engine.Task{}
				closeTask.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanClose,
					ch:     ch,
				}})
				result[2] = closeTask

				return result, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Len(t, tasks, 3)

		// Verify close effects
		assert.True(t, ch.closed, "channel should be closed")
		assert.Equal(t, lua.LNil, tasks[0].GetResumeValues()[0], "pending send should fail")
		assert.Equal(t, lua.LNil, tasks[1].GetResumeValues()[0], "pending receive should get nil")
	})

	t.Run("non-channel operations pass through", func(t *testing.T) {
		s := NewScheduler()

		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				regularTask := &engine.Task{}
				regularTask.SetYieldedValues([]lua.LValue{lua.LString("regular yield")})
				return []*engine.Task{regularTask}, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, "regular yield", tasks[0].GetYieldedValues()[0].String())
	})

	t.Run("error propagation", func(t *testing.T) {
		s := NewScheduler()

		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				return nil, assert.AnError
			},
		}

		tasks, err := s.Step(vm)
		assert.Error(t, err)
		assert.Nil(t, tasks)
	})
}

func TestScheduler_TaskQueue(t *testing.T) {
	t.Run("pending operation cleanup", func(t *testing.T) {
		s := NewScheduler()
		ch := newLuaChannel(0)

		// Fill pending queues
		senderTask := &engine.Task{}
		s.pushOperation(senderTask, &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("test"),
		})

		receiverTask := &engine.Task{}
		s.pushOperation(receiverTask, &chanOperation{
			opType: chanReceive,
			ch:     ch,
		})

		// Verify queues are populated
		assert.NotNil(t, s.senders[ch])
		assert.NotNil(t, s.receivers[ch])

		// Close channel
		closeTask := &engine.Task{}
		tasks := s.pushOperation(closeTask, &chanOperation{
			opType: chanClose,
			ch:     ch,
		})

		// Verify cleanup
		assert.Len(t, tasks, 3)
		assert.Nil(t, s.senders[ch])
		assert.Nil(t, s.receivers[ch])
		assert.True(t, ch.closed)
	})

	t.Run("object pool reuse", func(t *testing.T) {
		s := NewScheduler()
		ch := newLuaChannel(0)

		// Get initial objects from pool
		node1 := pendingPool.Get().(*pendingOp)
		queue1 := queuePool.Get().(*pendingQueue)

		// Reset and return to pool
		node1.reset()
		pendingPool.Put(node1)
		queue1.reset()
		queuePool.Put(queue1)

		// Use scheduler operations to reuse pool objects
		task := &engine.Task{}
		s.pushOperation(task, &chanOperation{
			opType: chanSend,
			ch:     ch,
			value:  lua.LString("test"),
		})

		// Verify objects were reused
		assert.NotNil(t, s.senders[ch])
		assert.Equal(t, task, s.senders[ch].head.task)
	})
}

func TestScheduler_EdgeCases(t *testing.T) {
	t.Run("zero capacity channel", func(t *testing.T) {
		s := NewScheduler()
		ch := newLuaChannel(0)

		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				senderTask := &engine.Task{}
				senderTask.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanSend,
					ch:     ch,
					value:  lua.LString("test"),
				}})
				return []*engine.Task{senderTask}, nil
			},
		}

		// Send should block without receiver
		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Empty(t, tasks)

		// Add receiver
		vm.stepFunc = func(tasks ...*engine.Task) ([]*engine.Task, error) {
			receiverTask := &engine.Task{}
			receiverTask.SetYieldedValues([]lua.LValue{&chanOperation{
				opType: chanReceive,
				ch:     ch,
			}})
			return []*engine.Task{receiverTask}, nil
		}

		tasks, err = s.Step(vm)
		assert.NoError(t, err)
		assert.Len(t, tasks, 2)
	})

	t.Run("closed channel operations", func(t *testing.T) {
		s := NewScheduler()
		ch := newLuaChannel(1)

		// Close channel
		closeTask := &engine.Task{}
		s.pushOperation(closeTask, &chanOperation{
			opType: chanClose,
			ch:     ch,
		})

		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				result := make([]*engine.Task, 2)

				// Try send on closed channel
				senderTask := &engine.Task{}
				senderTask.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanSend,
					ch:     ch,
					value:  lua.LString("test"),
				}})
				result[0] = senderTask

				// Try receive from closed channel
				receiverTask := &engine.Task{}
				receiverTask.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanReceive,
					ch:     ch,
				}})
				result[1] = receiverTask

				return result, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Len(t, tasks, 2)

		// Verify closed channel behavior
		assert.Equal(t, lua.LNil, tasks[0].GetResumeValues()[0], "send on closed channel should fail")
		assert.Equal(t, lua.LNil, tasks[1].GetResumeValues()[0], "receive from closed empty channel should return nil")
	})

	t.Run("multiple operations on same channel", func(t *testing.T) {
		s := NewScheduler()
		ch := newLuaChannel(1)

		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				result := make([]*engine.Task, 3)

				// First send - should buffer
				send1Task := &engine.Task{}
				send1Task.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanSend,
					ch:     ch,
					value:  lua.LString("first"),
				}})
				result[0] = send1Task

				// Second send - should block
				send2Task := &engine.Task{}
				send2Task.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanSend,
					ch:     ch,
					value:  lua.LString("second"),
				}})
				result[1] = send2Task

				// Receive - should get buffered value
				recvTask := &engine.Task{}
				recvTask.SetYieldedValues([]lua.LValue{&chanOperation{
					opType: chanReceive,
					ch:     ch,
				}})
				result[2] = recvTask

				return result, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)

		// Verify operation sequence
		foundFirst := false
		foundSecond := false
		for _, task := range tasks {
			vals := task.GetResumeValues()
			if len(vals) > 0 {
				if vals[0].String() == "first" {
					foundFirst = true
				}
				if vals[0] == lua.LBool(true) {
					foundSecond = true
				}
			}
		}
		assert.True(t, foundFirst, "should receive first value")
		assert.True(t, foundSecond, "second send should complete")
	})
}
