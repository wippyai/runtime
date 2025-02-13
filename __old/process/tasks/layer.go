package tasks

import (
	"sync/atomic"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

// taskSchedule represents a message that can be sent through the task layer
type taskSchedule struct {
	id      TaskID
	input   []lua.LValue
	channel chan engine.Result
}

// mixerLayer implements task management functionality
type mixerLayer struct {
	channels *channel.Layer
	outbox   *channel.Channel
	inbox    chan *taskSchedule // Shared channel for all tasks
	close    chan struct{}
	closed   int32
}

// newTaskMixer creates a new task management layer
func newTaskMixer(channels *channel.Layer, inbox chan *taskSchedule) *mixerLayer {
	return &mixerLayer{
		channels: channels,
		outbox:   nil, // created on demand
		inbox:    inbox,
		close:    make(chan struct{}, 1),
		closed:   int32(0),
	}
}

// Step implements the engine.Layer interface
func (m *mixerLayer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	processableTasks := tasks
	var outTasks []*engine.Task

	select {
	case <-m.close:
		if m.outbox != nil {
			if err := m.channels.Close(cvm.State().Context(), m.outbox); err != nil {
				return nil, err
			}
			m.outbox = nil
			drain := true
			for {
				if !drain {
					break
				}
				select {
				case <-m.inbox:
				default:
					drain = false
				}
			}
			atomic.CompareAndSwapInt32(&m.closed, 1, 0)
		}
	default:
	}

	boot := true
	for len(processableTasks) > 0 || boot {
		boot = false

		// Process tasks through next layer
		nextTasks, err := cvm.Step(processableTasks...)
		if err != nil {
			return nil, err
		}

		processableTasks = nil // Reset for next iteration

		// Process any yields from tasks
		for _, task := range nextTasks {
			if len(task.Yielded) == 0 {
				outTasks = append(outTasks, task)
				continue
			}

			// Check if this yield is requesting a channel
			if chr, ok := isChannelRequest(task.Yielded[len(task.Yielded)-1]); ok {
				// Spawn outbox channel on first use
				if m.outbox == nil {
					m.outbox = channel.Named("tasks", chr.bufferSize)
				}

				task.Resumed = []lua.LValue{channel.Wrap(task.Thread(), m.outbox)}
				processableTasks = append(processableTasks, task)
				continue
			}

			// Not our yield, pass it through
			outTasks = append(outTasks, task)
		}

		flush := false
		execute := make([]lua.LValue, 0)
		for {
			if flush {
				break
			}

			select {
			case schedule := <-m.inbox:
				if m.outbox == nil {
					m.outbox = channel.Named("tasks", cap(m.inbox))
				}
				execute = append(execute, newTask(cvm.State(), schedule))
				if len(execute) >= m.outbox.Slots() {
					flush = true
				}
			default:
				flush = true
			}
		}

		if len(execute) != 0 {
			if err := m.channels.Send(cvm.State().Context(), m.outbox, execute...); err != nil {
				return nil, err
			}
		}
	}

	return outTasks, nil
}

func (m *mixerLayer) closeChannel() {
	if atomic.CompareAndSwapInt32(&m.closed, 0, 1) {
		close(m.close)
	}
}
