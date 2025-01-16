package async

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
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

// NewAsyncRunner creates a new async runner layer
func NewAsyncRunner() *Runner {
	r := &Runner{results: make(chan taskEntry, 4096)}
	return r
}

// Step implements the engine.Layer interface
func (r *Runner) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	nextTasks := make([]*engine.Task, 0)

	for len(tasks) != 0 {
		for _, task := range tasks {
			if len(task.Yielded) == 0 {
				nextTasks = append(nextTasks, task)
				continue
			}

			// Check for async function wrapper
			if wrapper, ok := task.Yielded[len(task.Yielded)-1].(*FuncWrapper); ok {
				r.wait++
				task.Yielded = []lua.LValue{}
				go func() { r.results <- taskEntry{task: task, result: wrapper.Run()} }()
			} else {
				nextTasks = append(nextTasks, task)
			}
		}

		// try tasks that already ready
		nextTasks = append(nextTasks, r.flush(cvm.GetContext(), false)...)

		var err error
		tasks, err = cvm.Step(nextTasks...)
		if err != nil {
			return nil, err
		}

		if len(tasks) == 0 {
			if r.wait != 0 {
				// wait for some tasks to complete to unblock
				tasks = append(tasks, r.flush(cvm.GetContext(), true)...)
				if r.wait != 0 && len(tasks) == 0 {
					return nil, cvm.GetContext().Err() // intercepted
				}

				continue
			}

			// no more tasks to process
			return nil, nil
		}
	}

	return nil, nil
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
