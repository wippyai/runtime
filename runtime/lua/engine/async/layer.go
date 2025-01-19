package async

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
)

// Runner processes scheduled operations
type Runner struct {
	channels *channel.Runner
}

func NewAsyncRunner(channels *channel.Runner) *Runner {
	return &Runner{
		channels: channels,
	}
}

// Step implements the engine.Layer interface
func (r *Runner) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	// Process tasks through next layer first
	outTasks, err := cvm.Step(tasks...)
	if err != nil {
		return nil, err
	}

	if sch := GetScheduleChannel(cvm.GetContext()); sch != nil {
		select {
		case item := <-sch:
			// push data downstream to channel runner

			if item.ok {
				err := r.channels.Send(cvm.GetContext(), item.ch, item.value)
				if err != nil {
					return outTasks, nil // Log error but continue
				}
			} else {
				err := r.channels.Close(cvm.GetContext(), item.ch)
				if err != nil {
					return outTasks, nil // Log error but continue
				}
			}
		default:
			// No items to process
		}
	}

	return outTasks, nil
}
