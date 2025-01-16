package coroutine

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
)

type taskEntry struct {
	task   *engine.Task
	result Result
}

// Runner provides layer for handling async function wrappers
type Runner struct {
	wait    int
	results chan taskEntry
}

// NewCoroutineRunner creates a new async runner layer
func NewCoroutineRunner() *Runner {
	r := &Runner{results: make(chan taskEntry, 4096)}
	return r
}

// Step implements the engine.Layer interface
func (r *Runner) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	outTasks := make([]*engine.Task, 0)
	var err error
	boot := true
	for r.wait > 0 || boot {
		boot = false

		tasks = append(tasks, r.flush(cvm.GetContext(), len(tasks) == 0)...)

		tasks, err = cvm.Step(tasks...)
		if err != nil {
			return nil, err
		}

		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				if wrapper, ok := task.Yielded[len(task.Yielded)-1].(*FuncWrapper); ok {
					r.wait++

					go func(t *engine.Task, w *FuncWrapper) {
						r.results <- taskEntry{task: t, result: w.Run()}
					}(task, wrapper)
					continue
				}
			}

			outTasks = append(outTasks, task) // not our task
		}

		tasks = []*engine.Task{}
	}

	return outTasks, nil
}

func (r *Runner) flush(ctx context.Context, block bool) []*engine.Task {
	tasks := make([]*engine.Task, 0)
	for r.wait > 0 {
		if block {
			select {
			case entry := <-r.results:
				if entry.result.Err != nil {
					entry.task.RaiseError = entry.result.Err
				} else {
					entry.task.Resumed = entry.result.Values
				}

				r.wait--
				tasks = append(tasks, entry.task)
				continue
			case <-ctx.Done():
				return tasks
			}
		}

		select {
		case entry := <-r.results:
			if entry.result.Err != nil {
				entry.task.RaiseError = entry.result.Err
			} else {
				entry.task.Resumed = entry.result.Values
			}

			r.wait--
			tasks = append(tasks, entry.task)
		default:
			break
		}
	}

	return tasks
}
