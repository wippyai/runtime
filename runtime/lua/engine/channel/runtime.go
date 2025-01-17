package channel

import (
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
	next          []*opStep
	namedChannels map[string]*channelRef // Track named channels with reference counting
}

func NewChannelRunner() *Runner {
	return &Runner{
		queue:         engine.NewTaskQueue(),
		next:          make([]*opStep, 0),
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

func (r *Runner) Send(name string, values ...lua.LValue) error {
	ref, exists := r.namedChannels[name]
	if !exists {
		return fmt.Errorf("channel %s not found or not ready for data", name)
	}

	ch := ref.channel
	if (ch.size + len(values) - ref.count) > ch.capacity {
		return fmt.Errorf("unable to send %d values to channel %s, only %d slots available",
			len(values), name, ch.capacity-ch.size+ref.count)
	}

	for _, value := range values {
		next := ch.send(nil, value, nil)
		if next.yields && len(next.next) > 0 {
			if len(next.release) > 0 {
				r.updateChannelRefs(nil, next.release)
			}

			for _, result := range next.next {
				if result.state == nil {
					continue
				}
				r.next = append(r.next, result)
			}
		}
	}

	return nil
}

// Step handles channel operations while maintaining CVM compatibility
func (r *Runner) Step(vm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	var externalOps []*engine.Task

	for _, prepend := range r.next {
		task, err := vm.GetTask(prepend.state)
		if err != nil {
			return nil, fmt.Errorf("state not found: %w", err)
		}

		if prepend.err != nil {
			task.RaiseError = prepend.err
		} else {
			task.Resumed = prepend.values
		}

		r.queue.Push(task)
	}
	r.next = make([]*opStep, 0) // todo: it was not tested

	for _, task := range tasks {
		r.queue.Push(task)
	}

	boot := true
	for !r.queue.IsEmpty() || boot {
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

			r.updateChannelRefs(opNext.block, opNext.release)

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
func (r *Runner) updateChannelRefs(blocks, releases []*Channel) {
	for _, ch := range blocks {
		if ch.isNamed() {
			ref, exists := r.namedChannels[ch.name]
			if !exists {
				ref = &channelRef{channel: ch}
				r.namedChannels[ch.name] = ref
			}
			ref.count++
		}
	}

	for _, ch := range releases {
		if ch.isNamed() {
			if ref, exists := r.namedChannels[ch.name]; exists {
				ref.count--
				// Remove channel if no more references
				if ref.count == 0 {
					delete(r.namedChannels, ch.name)
				}
			}
		}
	}
}
