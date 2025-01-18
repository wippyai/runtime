package coroutine

import (
	"errors"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// Func is our simplified function format that just returns a Result
type Func func() Result

// Result represents possible outputs from async function
type Result struct {
	Values []lua.LValue
	Err    error
}

type FuncWrapper struct {
	fn Func
}

func (f *FuncWrapper) Type() lua.LValueType {
	return lua.LTFunction
}

func (f *FuncWrapper) String() string {
	return "async.func"
}

// Wrap wraps our Func into Lua-compatible format
func Wrap(L *lua.LState, fn Func) {
	L.Push(&FuncWrapper{fn: fn})
}

// Run runs the wrapped function and returns results/error
func (f *FuncWrapper) Run() Result {
	if f.fn == nil {
		return Result{Err: errors.New("function has already been executed")}
	}

	r := f.fn()
	f.fn = nil
	return r
}

// Runner provides layer for handling async function wrappers
type Runner struct {
}

// NewCoroutineRunner creates a new async runner layer
func NewCoroutineRunner() *Runner {
	return &Runner{}
}

// Step implements the engine.Layer interface
func (r *Runner) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	outTasks := make([]*engine.Task, 0)

	vmTasks, err := cvm.Step(tasks...)
	if err != nil {
		return nil, err
	}

	for _, task := range vmTasks {
		if len(task.Yielded) > 0 {
			if fw, ok := task.Yielded[len(task.Yielded)-1].(*FuncWrapper); ok {
				tCtx := task.Thread().Context()
				tg := engine.GetTaskGroup(tCtx)
				if tg == nil {
					return nil, errors.New("task group not found")
				}
				tg.Add(task.Thread())
				go func(t *engine.Task, w *FuncWrapper) {
					res := w.Run()
					_ = tg.Send(tCtx, engine.TaskResult{State: t.Thread(), Result: res.Values, Error: res.Err})
				}(task, fw)
				continue
			}
		}

		outTasks = append(outTasks, task) // not our tasks
	}

	tasks = []*engine.Task{}

	return outTasks, nil
}
