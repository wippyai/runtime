package coroutine

import (
	"errors"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// Func is our simplified function format that just returns a Result
type Func func() *engine.Result

// FuncWrapper encapsulates an asynchronous function that can be executed in Lua context
type FuncWrapper struct {
	fn Func
}

// Type returns the Lua type for this function wrapper
func (f *FuncWrapper) Type() lua.LValueType {
	return lua.LTFunction
}

func (f *FuncWrapper) String() string {
	return "async.func"
}

// Wrap wraps our Func into the Lua-compatible format
func Wrap(l *lua.LState, fn Func) {
	l.Push(&FuncWrapper{fn: fn})
}

// Run runs the wrapped function and returns results/error
func (f *FuncWrapper) Run() *engine.Result {
	if f.fn == nil {
		return engine.NewResult(nil, nil, errors.New("function has already been executed"))
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

	vmTasks, err := cvm.Step(tasks...)
	if err != nil {
		return nil, err
	}

	for _, task := range vmTasks {
		if len(task.Yielded) == 0 {
			outTasks = append(outTasks, task)
			continue
		}

		if fw, ok := task.Yielded[len(task.Yielded)-1].(*FuncWrapper); ok {
			thread := task.Thread()
			tg := engine.GetTaskGroup(thread.Context())

			if tg == nil {
				return nil, errors.New("task group not found")
			}
			tg.Add(thread)
			go func(w *FuncWrapper) {
				res := w.Run()
				res.State = thread
				_ = tg.Send(thread.Context(), res)
			}(fw)
			continue
		}

		outTasks = append(outTasks, task) // not our tasks
	}

	for i := 0; i < len(tasks); i++ {
		tasks[i] = nil
	}

	return outTasks, nil
}
