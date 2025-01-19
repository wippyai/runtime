package chromise

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"log"
)

// Runner processes scheduled operations
type Runner struct {
	channels *channel.Runner
}

func NewChromiseRunner(channels *channel.Runner) *Runner {
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

	// Get schedule channel from context
	if sch := GetScheduleChannel(cvm.GetContext()); sch != nil {
		// Non-blocking check of schedule channel
		select {
		case item := <-sch:
			tg := engine.GetTaskGroup(cvm.GetContext())
			if tg == nil {
				return outTasks, nil
			}

			if item.ok {
				err := r.channels.Send(cvm.GetContext(), item.ch, item.value)
				if err != nil {
					log.Printf("chromise: failed to send value: %s", err) // todo: make it better
					return outTasks, nil
				}
			} else {
				err := r.channels.Close(cvm.GetContext(), item.ch)
				if err != nil {
					log.Printf("chromise: failed to close channel: %s", err) // todo: make it better
					return outTasks, nil                                     // Log error but continue
				}
			}
		default:
			// No items to process
		}
	}

	return outTasks, nil
}
