package tasks

import (
	"errors"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

type Layer struct {
	chRunner   *channel.Runner
	bufferChan chan *task
	bufferSize int
}

type LayerOption func(*Layer)

func WithBufferSize(size int) LayerOption {
	return func(l *Layer) {
		l.bufferSize = size
	}
}

func NewLayer(chRunner *channel.Runner, opts ...LayerOption) *Layer {
	l := &Layer{
		chRunner:   chRunner,
		bufferSize: 256,
	}

	for _, opt := range opts {
		opt(l)
	}

	l.bufferChan = make(chan *task, l.bufferSize)
	return l
}

// Schedule pushes a new task to the buffer and returns a channel for the response
func (l *Layer) Schedule(tg *engine.TaskGroup, value lua.LValue) (chan response, error) {
	respChan := make(chan response, 1)
	t := &task{
		value:    []lua.LValue{value},
		respChan: respChan,
	}

	select {
	case l.bufferChan <- t:
		return respChan, nil
	default:
		// Buffer is full
		return nil, errors.New("task buffer full")
	}
}

func (l *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {

	return cvm.Step(tasks...)

	// Just try to flush any buffered tasks
	if err := l.tryFlush(); err != nil {
		return nil, err
	}

	// Pass through all tasks without modification
	return tasks, nil
}

// tryFlush attempts to flush tasks from buffer to the named channel
func (l *Layer) tryFlush() error {
	// Check if channel is open and get capacity
	openChannels := l.chRunner.GetActiveChannels()
	var taskChannel *channel.ActiveChannel
	for _, ch := range openChannels {
		if ch.Name == Channel {
			taskChannel = &ch
			break
		}
	}

	if taskChannel == nil || taskChannel.Slots == 0 {
		return nil // No channel or no capacity
	}

	// Calculate how many tasks we can send
	available := taskChannel.Slots
	values := make([]lua.LValue, 0, available)

	// Keep reading while we have space and there are buffered tasks
	reading := true
	for reading && len(values) < available {
		select {
		case t := <-l.bufferChan:
			if len(t.value) > 0 {
				values = append(values, t.value[0])
			}
		default:
			reading = false // No more buffered tasks available
		}
	}

	if len(values) > 0 {
		return l.chRunner.SendToOpen(nil, nil, Channel, values...)
	}
	return nil
}
