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
	queue         *engine.TaskQueue
	namedChannels map[string]*channelRef // Track named channels with reference counting
}

func NewChannelRunner() *Runner {
	return &Runner{
		queue:         engine.NewTaskQueue(),
		namedChannels: make(map[string]*channelRef),
	}
}

// GetOpenChannels returns a map of named channels currently waiting for data
func (r *Runner) GetOpenChannels() []OpenChannel {
	result := make([]OpenChannel, 0, len(r.namedChannels))
	for name, ref := range r.namedChannels {
		result = append(result, OpenChannel{
			Name:  name,
			Slots: ref.channel.capacity - ref.channel.size + ref.count,
			Refs:  ref.count,
		})
	}
	return result
}

// Send is NOT thread safe, it should only be called by another layer during execution step.
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

// updateChannelRefs handles reference counting for channels todo: deprecate
func (r *Runner) updateChannelRefs(tg *engine.TaskGroup, blocks, releases []*Channel) {
	for _, ch := range blocks {
		if ch.isNamed() {
			ref, exists := r.namedChannels[ch.name]
			if !exists {
				ref = &channelRef{channel: ch}
				r.namedChannels[ch.name] = ref
			}

			ref.count++

			if tg != nil {
				tg.Add(nil)
			}
		}
	}

	for _, ch := range releases {
		if ch.isNamed() {
			if ref, exists := r.namedChannels[ch.name]; exists {
				ref.count--

				if ref.count == 0 {
					delete(r.namedChannels, ch.name)
				}

				if tg != nil {
					tg.Remove(nil)
				}
			}
		}
	}
}
