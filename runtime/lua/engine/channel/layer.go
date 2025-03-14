// Package channel provides channel-based communication primitives for the Lua runtime engine
package channel

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// Layer maintains State for channel operations and provides thread-safe
// channel management within the Lua runtime engine.
type Layer struct {
}

// NewChannelLayer creates a new Layer instance with initialized task queue
// and channel tracking.
func NewChannelLayer() *Layer {
	return &Layer{}
}

func (r *Layer) InitUnitOfWork(uw engine.UnitOfWork) {
	// configure uow scope for the layer
	uw.Values().Set(uowContext, &layerContext{
		queue:    engine.NewTaskQueue(),
		channels: make(map[*Channel]int),
	})
}

// Step handles channel operations while maintaining CVM compatibility.
// This method processes tasks in batches and manages channel operations
// through the virtual machine.
func (r *Layer) Step(vm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	uw := engine.GetUnitOfWork(vm.State().Context())
	if uw == nil {
		return nil, fmt.Errorf("unit of work not found on context")
	}

	lCtx := ensureLayerContext(uw)
	if lCtx == nil {
		return nil, fmt.Errorf("layer context not found in unit of work")
	}
	var externalOps []*engine.Task

	for _, task := range tasks {
		lCtx.queue.Push(task)
	}

	boot := true
	for !lCtx.queue.IsEmpty() || boot { // we want to rotate channels as close to VM as possible
		boot = false

		var batch []*engine.Task
		for !lCtx.queue.IsEmpty() {
			batch = append(batch, lCtx.queue.Pop())
		}

		vmTasks, err := vm.Step(batch...)
		if err != nil {
			return nil, err
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

			updateRefs(uw, opNext.block, opNext.release)

			if opNext.yields && len(opNext.next) > 0 {
				for _, result := range opNext.next {
					t, err := vm.GetTask(result.State)
					if err != nil {
						return nil, fmt.Errorf("State not found!: %w", err)
					}

					if result.Error != nil {
						t.RaiseError = result.Error
					} else {
						t.Resumed = result.Result
					}

					lCtx.queue.Push(t)
				}
			}
		}
	}

	return externalOps, nil
}

// updateChannelRefs handles reference counting for channels
func updateRefs(uw engine.UnitOfWork, blocks, releases []*Channel) {
	// never send or close outside of layer sequences! always use tasks.schedule!
	lCtx := getLayerContext(uw)
	if lCtx == nil {
		// invalid env setup
		panic("layer context not found in unit of work")
	}

	for _, ch := range blocks {
		_, exists := lCtx.channels[ch]
		if !exists {
			lCtx.channels[ch] = 0
		}

		lCtx.channels[ch]++
		if ch.isNamed() {
			uw.Tasks().Add()
		}
	}

	for _, ch := range releases {
		if _, exists := lCtx.channels[ch]; exists {
			lCtx.channels[ch]--
			if lCtx.channels[ch] == 0 {
				delete(lCtx.channels, ch)
			}

			if ch.isNamed() {
				uw.Tasks().Done()
			}
		}
	}
}

func send(ctx context.Context, ch *Channel, values ...lua.LValue) error {
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return fmt.Errorf("unit of work not found on context")
	}

	for _, value := range values {
		next := ch.send(nil, value, nil)
		if next.yields && len(next.next) > 0 {
			if len(next.release) > 0 {
				updateRefs(uw, next.block, next.release)
			}

			for _, upd := range next.next {
				if upd.State == nil {
					// no one waits for us
					continue
				}

				err := uw.Tasks().Send(ctx, upd)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func closeChannel(ctx context.Context, ch *Channel) error {
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return fmt.Errorf("unit of work not found on context")
	}

	next := ch.close(nil)
	if next.yields && len(next.next) > 0 {
		if len(next.release) > 0 {
			updateRefs(uw, next.block, next.release)
		}

		for _, upd := range next.next {
			if upd.State == nil {
				// no one waits for us
				continue
			}

			err := uw.Tasks().Send(ctx, upd)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
