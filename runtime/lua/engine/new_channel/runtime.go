package channel

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

type VM interface {
	GetTask(thread *lua.LState) (*engine.Task, error)
	Step(tasks ...*engine.Task) ([]*engine.Task, error)
}

type Runtime struct {
	queue *engine.TaskQueue
}

func NewRuntime() *Runtime {
	return &Runtime{
		queue: engine.NewTaskQueue(),
	}
}

// Step handles channel operations while maintaining VM compatibility
func (r *Runtime) Step(vm VM, tasks ...*engine.Task) ([]*engine.Task, error) {
	var externalOps []*engine.Task

	for _, task := range tasks {
		r.queue.Push(task)
	}

	for !r.queue.IsEmpty() {
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

			op, ok := task.Yielded[0].(*op)
			if !ok {
				externalOps = append(externalOps, task)
				continue
			}

			if next := r.handleChannelOp(task, op); next.yields && len(next.results) > 0 {
				for _, result := range next.results {
					task, err := vm.GetTask(result.task)
					if err != nil {
						return nil, fmt.Errorf("task not found: %w", err)
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

	// delegate to parent layer
	return externalOps, nil
}

func (r *Runtime) handleChannelOp(task *engine.Task, op *op) onNext {
	switch op.kind {
	case sendOp:
		return op.ch.send(task.Thread(), op.value, nil)
	case receiveOp:
		return op.ch.receive(task.Thread(), nil)
	}

	return onNext{}
}
