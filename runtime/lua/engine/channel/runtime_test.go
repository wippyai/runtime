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

func TestScheduler(t *testing.T) {
	t.Run("pass through non-channel tasks", func(t *testing.T) {
		s := NewRuntime()

		normalTask := &engine.Task{
			Yielded: []lua.LValue{lua.LString("normal")},
		}

		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				return []*engine.Task{normalTask}, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, normalTask, tasks[0])
	})

	t.Run("unbuffered channel send/receive", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(0)

		senderTask := &engine.Task{}
		receiverTask := &engine.Task{}

		var phase int
		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				switch phase {
				case 0:
					// Initial tasks
					senderTask.Yielded = []lua.LValue{&chanOperation{
						opType: chanSend,
						ch:     ch,
						value:  lua.LString("test"),
					}}
					receiverTask.Yielded = []lua.LValue{&chanOperation{
						opType: chanReceive,
						ch:     ch,
					}}
					phase++
					return []*engine.Task{senderTask, receiverTask}, nil
				case 1:
					// Channel operations completed
					phase++
					// Verify results
					assert.Equal(t, lua.LBool(true), senderTask.Resumed[0], "send should succeed")
					assert.Equal(t, lua.LString("test"), receiverTask.Resumed[0], "receive should get value")
					return nil, nil
				}
				return nil, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Empty(t, tasks, "all tasks should be processed")
	})

	t.Run("buffered channel operations", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(2)

		sender1 := &engine.Task{}
		sender2 := &engine.Task{}
		sender3 := &engine.Task{}
		receiver := &engine.Task{}

		var phase int
		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				switch phase {
				case 0:
					// First two sends (should succeed immediately)
					sender1.Yielded = []lua.LValue{&chanOperation{
						opType: chanSend,
						ch:     ch,
						value:  lua.LString("msg1"),
					}}
					sender2.Yielded = []lua.LValue{&chanOperation{
						opType: chanSend,
						ch:     ch,
						value:  lua.LString("msg2"),
					}}
					phase++
					return []*engine.Task{sender1, sender2}, nil
				case 1:
					// Third send (should block) and receive
					sender3.Yielded = []lua.LValue{&chanOperation{
						opType: chanSend,
						ch:     ch,
						value:  lua.LString("msg3"),
					}}
					receiver.Yielded = []lua.LValue{&chanOperation{
						opType: chanReceive,
						ch:     ch,
					}}
					phase++
					return []*engine.Task{sender3, receiver}, nil
				case 2:
					// Verify results
					assert.Equal(t, lua.LBool(true), sender1.Resumed[0], "first send should succeed")
					assert.Equal(t, lua.LBool(true), sender2.Resumed[0], "second send should succeed")
					assert.Equal(t, lua.LString("msg1"), receiver.Resumed[0], "receiver should get first message")
					phase++
					return nil, nil
				}
				return nil, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Empty(t, tasks)
	})

	t.Run("channel close operation", func(t *testing.T) {
		s := NewRuntime()
		ch := newChannel(1)

		sender := &engine.Task{}
		closer := &engine.Task{}
		receiver := &engine.Task{}

		var phase int
		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				switch phase {
				case 0:
					// Send a message and close channel
					sender.Yielded = []lua.LValue{&chanOperation{
						opType: chanSend,
						ch:     ch,
						value:  lua.LString("final"),
					}}
					closer.Yielded = []lua.LValue{&chanOperation{
						opType: chanClose,
						ch:     ch,
					}}
					phase++
					return []*engine.Task{sender, closer}, nil
				case 1:
					// Try receive after close
					receiver.Yielded = []lua.LValue{&chanOperation{
						opType: chanReceive,
						ch:     ch,
					}}
					phase++
					return []*engine.Task{receiver}, nil
				case 2:
					// Verify results
					assert.Equal(t, lua.LBool(true), sender.Resumed[0], "send should succeed")
					assert.Equal(t, lua.LBool(true), closer.Resumed[0], "close should succeed")
					assert.Equal(t, lua.LString("final"), receiver.Resumed[0], "receive should get final message")
					assert.Equal(t, lua.LBool(true), receiver.Resumed[1], "receive should indicate success")
					phase++
					return nil, nil
				}
				return nil, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Empty(t, tasks)
	})

	t.Run("inbox channel operations", func(t *testing.T) {
		s := NewRuntime()
		ch := Named("test-signal")

		receiver := &engine.Task{}

		var phase int
		vm := &mockVM{
			stepFunc: func(tasks ...*engine.Task) ([]*engine.Task, error) {
				switch phase {
				case 0:
					// Setup receiver
					receiver.Yielded = []lua.LValue{&chanOperation{
						opType: chanReceive,
						ch:     ch,
					}}
					phase++
					return []*engine.Task{receiver}, nil
				case 1:
					// Verify signal was registered
					signals := s.ActiveSignals()
					assert.Contains(t, signals, "test-signal", "signal should be registered")

					// Send external message
					tasks, _ := s.Send("test-signal", lua.LString("external"))
					assert.Len(t, tasks, 1, "receiver should be resumed")

					// Verify receiver got message
					assert.Equal(t, lua.LString("external"), receiver.Resumed[0], "receiver should get message")
					assert.Equal(t, lua.LBool(true), receiver.Resumed[1], "receive should indicate success")

					// Verify signal was unregistered
					signals = s.ActiveSignals()
					assert.NotContains(t, signals, "test-signal", "signal should be unregistered")

					phase++
					return nil, nil
				}
				return nil, nil
			},
		}

		tasks, err := s.Step(vm)
		assert.NoError(t, err)
		assert.Empty(t, tasks)
	})
}
