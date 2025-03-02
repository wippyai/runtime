package coroutine

import (
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"runtime/debug"
)

// Func is our simplified function format that just returns a Update
type Func func() *engine.Update

// funcWrapper encapsulates an asynchronous function that can be executed in Lua context
type funcWrapper struct {
	fn Func
}

// Type returns the Lua type for this function wrapper
func (f *funcWrapper) Type() lua.LValueType {
	return lua.LTFunction
}

func (f *funcWrapper) String() string {
	return "async.func"
}

// Wrap wraps our Func into the Lua-compatible format
func Wrap(l *lua.LState, fn Func) {
	l.Push(&funcWrapper{fn: fn})
}

// Run runs the wrapped function and returns results/error
func (f *funcWrapper) Run() *engine.Update {
	if f.fn == nil {
		return engine.NewUpdate(nil, nil, errors.New("function has already been executed"))
	}

	r := f.fn()
	f.fn = nil
	return r
}

// Layer provides layer for handling async function wrappers
type Layer struct {
}

// NewCoroutineLayer creates a new async runner layer
func NewCoroutineLayer() *Layer {
	return &Layer{}
}

// Step implements the engine.Layer interface
func (r *Layer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	outTasks := make([]*engine.Task, 0)

	uw := engine.GetUnitOfWork(cvm.State().Context())
	if uw == nil {
		return nil, errors.New("unit of work not found")
	}

	vmTasks, err := cvm.Step(tasks...)
	if err != nil {
		return nil, err
	}

	for _, task := range vmTasks {
		if len(task.Yielded) == 0 {
			outTasks = append(outTasks, task)
			continue
		}

		if fw, ok := task.Yielded[len(task.Yielded)-1].(*funcWrapper); ok {
			thread := task.Thread()

			uw.Run(func(uw engine.UnitOfWork) {
				defer func() {
					if r := recover(); r != nil {
						var panicErr error
						if err, ok := r.(error); ok {
							panicErr = err
						} else {
							panicErr = fmt.Errorf("%v, %s", r, debug.Stack())
						}

						res := engine.NewUpdate(nil, nil, panicErr)
						res.State = thread
						_ = uw.Tasks().Send(thread.Context(), res)
					}
				}()

				res := fw.Run()
				res.State = thread

				_ = uw.Tasks().Send(thread.Context(), res)
			})

			continue
		}

		outTasks = append(outTasks, task) // not our tasks
	}

	for i := 0; i < len(tasks); i++ {
		tasks[i] = nil
	}

	return outTasks, nil
}
