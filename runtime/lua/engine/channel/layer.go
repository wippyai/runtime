package channel

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

type OpenChannel struct {
	Name  string
	Slots int
	Refs  int
}

// channelRef tracks references to a named channel
type channelRef struct {
	channel *Channel
	count   int
}

// Runner maintains state for channel operations
type Runner struct {
	queue    *engine.TaskQueue
	channels map[*Channel]int // Track named channels with reference counting
}

func NewChannelRunner() *Runner {
	return &Runner{
		queue:    engine.NewTaskQueue(),
		channels: make(map[*Channel]int),
	}
}

// GetActiveChannels returns all channels that currently block execution.
func (r *Runner) GetActiveChannels() []OpenChannel {
	result := make([]OpenChannel, 0, len(r.channels))
	for ch, refs := range r.channels {
		result = append(result, OpenChannel{
			Name:  ch.name,
			Slots: ch.capacity - ch.size + refs,
			Refs:  refs,
		})
	}
	return result
}

// Send is NOT thread safe, it should only be called by another layer during execution step inside TG context.
func (r *Runner) Send(ctx context.Context, ch *Channel, values ...lua.LValue) error {
	tg := engine.GetTaskGroup(ctx)
	if tg == nil {
		return fmt.Errorf("task group not found on context")
	}

	for _, value := range values {
		next := ch.send(nil, value, nil)

		if next.yields && len(next.next) > 0 {
			if len(next.release) > 0 {
				r.updateChannelRefs(tg, next.block, next.release)
			}

			for _, result := range next.next {
				if result.state == nil {
					// no one waits for us
					continue
				}

				tg.Add(result.state)
				err := tg.Send(ctx, engine.TaskResult{
					State:  result.state,
					Result: result.values,
					Error:  result.err,
				})

				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// Close is NOT thread safe, it should only be called by another layer during execution step.
func (r *Runner) Close(ctx context.Context, ch *Channel) error {
	tg := engine.GetTaskGroup(ctx)
	if tg == nil {
		return fmt.Errorf("task group not found on context")
	}

	next := ch.close(nil)
	if next.yields && len(next.next) > 0 {
		if len(next.release) > 0 {
			r.updateChannelRefs(tg, next.block, next.release)
		}

		for _, result := range next.next {
			if result.state == nil {
				// no one waits for us
				continue
			}

			tg.Add(result.state)
			err := tg.Send(ctx, engine.TaskResult{
				State:  result.state,
				Result: result.values,
				Error:  result.err,
			})

			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Step handles channel operations while maintaining CVM compatibility, todo: deprecate
func (r *Runner) Step(vm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	tg := engine.GetTaskGroup(vm.GetContext())
	var externalOps []*engine.Task

	for _, task := range tasks {
		r.queue.Push(task)
	}

	boot := true
	for !r.queue.IsEmpty() || boot { // we want to rotate channels as close to VM as possible
		boot = false

		var batch []*engine.Task
		for !r.queue.IsEmpty() {
			batch = append(batch, r.queue.Pop())
		}

		vmTasks, err := vm.Step(batch...)
		if err != nil {
			return nil, fmt.Errorf("vm step failed: %w", err)
		}

		for _, task := range vmTasks {
			if len(task.Yielded) == 0 {
				continue
			}

			// when we yield from method Lua CVM preserves func args, remember that.
			value := task.Yielded[len(task.Yielded)-1]
			opNext, ok := value.(*onNext)
			if !ok {
				externalOps = append(externalOps, task)
				continue
			}

			r.updateChannelRefs(tg, opNext.block, opNext.release)

			if opNext.yields && len(opNext.next) > 0 {
				for _, result := range opNext.next {
					task, err := vm.GetTask(result.state)
					if err != nil {
						return nil, fmt.Errorf("state not found!: %w", err)
					}

					if result.err != nil {
						task.RaiseError = result.err
					} else {
						task.Resumed = result.values
					}

					r.queue.Push(task)
				}
			}
		}
	}

	return externalOps, nil
}

// updateChannelRefs handles reference counting for channels
func (r *Runner) updateChannelRefs(tg *engine.TaskGroup, blocks, releases []*Channel) {
	for _, ch := range blocks {
		_, exists := r.channels[ch]
		if !exists {
			r.channels[ch] = 0
		}

		r.channels[ch]++
		if ch.isNamed() && tg != nil {
			tg.Add(nil)
		}
	}

	for _, ch := range releases {
		if _, exists := r.channels[ch]; exists {
			r.channels[ch]--
			if r.channels[ch] == 0 {
				delete(r.channels, ch)
			}

			if ch.isNamed() && tg != nil {
				tg.Remove(nil)
			}
		}
	}
}
