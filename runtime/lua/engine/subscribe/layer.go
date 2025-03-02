package subscribe

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

// op represents a topic operation (message or unsubscribe)
type op struct {
	topic  string
	unsub  bool
	values []lua.LValue
}

// Layer implements the subscription processing middleware
type Layer struct {
}

// NewSubscribeLayer creates a new subscription processing layer
func NewSubscribeLayer() *Layer {
	return &Layer{}
}

// InitUnitOfWork initializes the subscription context for a new unit of work
func (l *Layer) InitUnitOfWork(uw engine.UnitOfWork) {
	// Initialize the layer context in unit of work
	ensureLayerContext(uw)
}

// Step implements the engine.Layer interface
func (l *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	// Get unit of work from VM context
	ctx := cvm.State().Context()
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return nil, fmt.Errorf("unit of work not found")
	}

	lCtx := getLayerContext(uw)
	if lCtx == nil {
		return nil, fmt.Errorf("layer context not found in unit of work")
	}

	var outTasks []*engine.Task
	processableTasks := tasks
	boot := true

	// Continue processing while we have tasks or during the boot phase
	for len(processableTasks) > 0 || boot {
		if boot {
			boot = false
			// Process message queue in boot stage
			for e := lCtx.messageQueue.Front(); e != nil; {
				msg := e.Value.(*op)
				nextElem := e.Next()

				if sub, exists := lCtx.subs.get(msg.topic); exists {
					if msg.unsub {
						if err := channel.Close(cvm.State(), sub.channel); err != nil {
							return nil, fmt.Errorf("close error: %w", err)
						}

						// we are fine to ignore this error since channel might not be subscribed yet
						_ = lCtx.subs.remove(sub.channel)
					} else {
						if err := channel.Send(cvm.State(), sub.channel, msg.values...); err != nil {
							return nil, fmt.Errorf("send error: %w", err)
						}
					}

					lCtx.messageQueue.Remove(e)
				}

				e = nextElem
			}
		}

		// Process through CVM
		nextTasks, err := cvm.Step(processableTasks...)
		if err != nil {
			return nil, err
		}

		// Clear processable tasks and prepare for potential new yields
		processableTasks = nil
		hasYields := false

		// Process yields and collect tasks
		for _, task := range nextTasks {
			if len(task.Yielded) == 0 {
				outTasks = append(outTasks, task)
				continue
			}

			lastYield := task.Yielded[len(task.Yielded)-1]

			// Handle subscription requests
			if req, ok := isSubscriptionRequest(lastYield); ok {
				hasYields = true
				sub, err := lCtx.subs.add(req.topic, req.channel)

				if err != nil {
					task.RaiseError = err
				} else {
					task.Resumed = []lua.LValue{channel.Wrap(task.Thread(), sub.channel)}
				}

				// Add to processable tasks for next iteration instead of immediately processing
				processableTasks = append(processableTasks, task)
				continue
			}

			// Handle unsubscribe requests
			if req, ok := isUnsubscribeRequest(lastYield); ok {
				hasYields = true
				err := lCtx.subs.remove(req.channel)
				if err != nil {
					task.RaiseError = err
				} else {
					if err := channel.Close(cvm.State(), req.channel); err != nil {
						task.RaiseError = err
					} else {
						task.Resumed = []lua.LValue{lua.LTrue}
					}
				}

				// Add to processable tasks for next iteration instead of immediately processing
				processableTasks = append(processableTasks, task)
				continue
			}

			outTasks = append(outTasks, task)
		}

		// Exit if no more yields to process
		if !hasYields {
			break
		}
	}

	return outTasks, nil
}

func isSubscriptionRequest(v lua.LValue) (*subscribe, bool) {
	if req, ok := v.(*subscribe); ok {
		return req, true
	}
	return nil, false
}

func isUnsubscribeRequest(v lua.LValue) (*unsubscribe, bool) {
	if req, ok := v.(*unsubscribe); ok {
		return req, true
	}
	return nil, false
}
