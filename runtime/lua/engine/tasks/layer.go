package tasks

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"log"
)

type TaskID = string

// taskSchedule represents a message that can be sent through the task layer
type taskSchedule struct {
	id      TaskID
	input   lua.LValue
	channel chan coroutine.Result
}

// Mixer implements task management functionality
type Mixer struct {
	channels *channel.Runner
	outbox   *channel.Channel
	inbox    chan *taskSchedule // Shared channel for all tasks
	close    chan struct{}
}

// NewMixer creates a new task management layer
func NewMixer(channels *channel.Runner, inboxSize int) *Mixer {
	return &Mixer{
		channels: channels,
		outbox:   nil, // created on demand
		inbox:    make(chan *taskSchedule, inboxSize),
		close:    make(chan struct{}),
	}
}

func (m *Mixer) Send(ctx context.Context, id TaskID, input lua.LValue) (chan coroutine.Result, error) {
	tg := engine.GetTaskGroup(ctx)
	if tg == nil {
		return nil, errors.New("no task group found in context") // todo: add predefined errors
	}

	tg.WakeUp()

	ret := make(chan coroutine.Result, 1)
	m.inbox <- &taskSchedule{id: id, input: input, channel: ret}
	return ret, nil
}

// Step implements the engine.Layer interface
func (m *Mixer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	tg := engine.GetTaskGroup(cvm.GetContext())
	if tg == nil {
		return nil, errors.New("no task group found in context") // todo: add predefined errors
	}

	processableTasks := tasks
	var outTasks []*engine.Task

	select {
	case <-m.close:
		if err := m.channels.Close(cvm.GetContext(), m.outbox); err != nil {
			return nil, err
		}
		m.outbox = nil
	default:
	}

	for len(processableTasks) > 0 {
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
				// Create outbox channel on first use
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

		// Check inbox for any new tasks while processing
		// todo: add batching support
		select {
		case schedule := <-m.inbox:
			log.Printf("Received task: %s", schedule.id)
			if m.outbox == nil {
				m.outbox = channel.Named("tasks", cap(m.inbox))
			}

			// Send input through channel runner
			if err := m.channels.Send(cvm.GetContext(), m.outbox, newTask(cvm.State(), schedule)); err != nil {
				return nil, err
			}
		default:
			// No new tasks
		}
	}

	return outTasks, nil
}

// Close closes the outbox channel and frees task on any block.
func (m *Mixer) Close(ctx context.Context) error {
	tg := engine.GetTaskGroup(ctx)
	if tg == nil {
		return errors.New("no task group found in context") // todo: add predefined errors
	}
	close(m.close)
	tg.WakeUp()

	return nil
}
