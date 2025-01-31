// Package channel provides channel-based communication primitives for the Lua runtime engine
package channel

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// ActiveChannel represents a channel that currently blocks execution,
// containing its current state and reference information.
type ActiveChannel struct {
	Name  string // Channel identifier
	Slots int    // Available slots in the channel
	Refs  int    // Number of current references
}

// Layer maintains state for channel operations and provides thread-safe
// channel management within the Lua runtime engine.
type Layer struct {
	queue    *engine.TaskQueue
	channels map[*Channel]int // Track named channels with reference counting
}

// NewChannelLayer creates a new Layer instance with initialized task queue
// and channel tracking.
func NewChannelLayer() *Layer {
	return &Layer{
		queue:    engine.NewTaskQueue(),
		channels: make(map[*Channel]int),
	}
}

// GetActiveChannels returns all channels that currently block execution.
// Each returned ActiveChannel contains the channel's name, available slots,
// and current reference count.
func (r *Layer) GetActiveChannels() []ActiveChannel {
	result := make([]ActiveChannel, 0, len(r.channels))
	for ch, refs := range r.channels {
		result = append(result, ActiveChannel{
			Name:  ch.name,
			Slots: ch.capacity - ch.size + refs,
			Refs:  refs,
		})
	}
	return result
}

// Send sends values to a channel within the context of a task group.
// This method is NOT thread safe and should only be called by another layer
// during execution step inside the TaskGroup context.
func (r *Layer) Send(ctx context.Context, ch *Channel, values ...lua.LValue) error {
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
				err := tg.Send(ctx, engine.NewResult(
					result.state,
					result.values,
					result.err,
				))

				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// Close closes a channel within the context of a task group.
// This method is NOT thread safe and should only be called by another layer
// during an execution step.
func (r *Layer) Close(ctx context.Context, ch *Channel) error {
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
			err := tg.Send(ctx, engine.NewResult(
				result.state,
				result.values,
				result.err,
			))

			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Step handles channel operations while maintaining CVM compatibility.
// This method processes tasks in batches and manages channel operations
// through the virtual machine.
func (r *Layer) Step(vm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
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

			r.updateChannelRefs(engine.GetTaskGroup(task.Thread().Context()), opNext.block, opNext.release)

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
func (r *Layer) updateChannelRefs(tg *engine.TaskGroup, blocks, releases []*Channel) {
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
