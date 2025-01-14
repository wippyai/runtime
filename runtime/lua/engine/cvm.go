package engine

import (
	"context"
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"strings"
)

// this vm is NOT thread safe, external synchronization is required
type CoroutineVM struct {
	ctx   context.Context
	vm    *VM
	tasks []*Task
	queue *TaskQueue
}

func NewCVM(
	ctx context.Context,
	log *zap.Logger,
	opts ...Option,
) (*CoroutineVM, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required for async VMs")
	}

	vm, err := NewVM(log, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	avm := &CoroutineVM{
		ctx:   ctx,
		vm:    vm,
		tasks: make([]*Task, 0),
		queue: NewTaskQueue(),
	}
	avm.vm.state.SetContext(ctx)
	avm.bindCoroutines()

	return avm, nil
}

func (e *CoroutineVM) DoString(script, scriptName string, args ...lua.LValue) error {
	chunk, err := e.vm.state.Load(strings.NewReader(script), scriptName)
	if err != nil {
		return err
	}

	task, err := e.createCoroutine(chunk)
	if err != nil {
		return err
	}
	task.Resumed = args

	return err
}

// Mount loads a function mounts (executes) provided function(s) prototype. Use it to share VCM code.
func (e *CoroutineVM) Mount(proto *lua.FunctionProto, scriptName string) error {
	return e.vm.Mount(proto, scriptName)
}

func (e *CoroutineVM) bindCoroutines() {
	coTable := e.vm.state.GetGlobal("coroutine").(*lua.LTable)

	e.vm.state.SetField(coTable, "spawn", e.vm.state.NewFunction(func(L *lua.LState) int {
		fnValue := L.Get(1)

		if fnValue.Type() != lua.LTFunction {
			L.RaiseError("coroutine.spawn() requires a function argument")
			return 0
		}

		if fn, ok := fnValue.(*lua.LFunction); ok {
			if fn.IsG || len(fn.Upvalues) > 0 {
				for _, upval := range fn.Upvalues {
					if _, isThread := upval.Value().(*lua.LState); isThread {
						L.RaiseError("cannot spawn vm coroutines")
						return 0
					}
				}
			}

			task, err := e.createCoroutine(fn)
			if err != nil {
				L.RaiseError("failed to spawn coroutine: %v", err)
				return 0
			}

			L.Push(task.thread)
			return 1
		}

		L.RaiseError("internal error: function cast failed")
		return 0
	}))

	// Modify resume to prevent resuming VM threads
	oldResume := coTable.RawGetString("resume").(*lua.LFunction)
	e.vm.state.SetField(coTable, "resume", e.vm.state.NewFunction(func(L *lua.LState) int {
		co := L.CheckThread(1)
		L.Push(oldResume)
		L.Push(co)
		L.Call(1, lua.MultRet)
		return L.GetTop() - 1
	}))
}

func (e *CoroutineVM) createCoroutine(fn *lua.LFunction) (*Task, error) {
	thread, cancel := e.vm.state.NewThread()

	task := &Task{
		l:      e.vm.state,
		thread: thread,
		cancel: cancel,
		fn:     fn,
		State:  -1,
	}

	e.tasks = append(e.tasks, task)
	e.queue.Push(task)

	return task, nil
}

func (e *CoroutineVM) GetTask(thread *lua.LState) (*Task, error) {
	for _, task := range e.tasks {
		if task.thread == thread {
			return task, nil
		}
	}
	return nil, fmt.Errorf("task not found")
}

func (e *CoroutineVM) Start(funcName string, args ...lua.LValue) error {
	fn, ok := e.vm.exported[funcName]
	if !ok {
		return fmt.Errorf("function %q not found", funcName)
	}

	task, err := e.createCoroutine(fn)
	if err != nil {
		return fmt.Errorf("failed to create coroutine: %w", err)
	}
	task.Resumed = args

	return nil
}

func (e *CoroutineVM) Step(tasks ...*Task) (result []*Task, finalErr error) {
	// at the moment Lua5.1 does not allow to use yields as part of pcall, doing that will cause engine panic
	// so we need to recover from it in case of user error
	// todo: properly isolate this issue and make upstream PR
	defer func() {
		if r := recover(); r != nil {
			finalErr = fmt.Errorf("panic: %v", r)
		}
	}()

	// Add tasks to queue
	for _, t := range tasks {
		e.queue.Push(t)
	}

	var state lua.ResumeState
	var err error
	var values []lua.LValue

	yeildedTasks := make([]*Task, 0)

	for !e.queue.IsEmpty() {
		task := e.queue.Pop()

		switch task.State {
		case -1:
			// Start
			state, err, values = e.vm.state.Resume(task.thread, task.fn)
			if err != nil {
				task.cancel()
				_ = e.removeTask(task)
				return nil, fmt.Errorf("error starting task: %v", err)
			}
		case lua.ResumeOK:
			// done
			if err := e.removeTask(task); err != nil {
				return nil, fmt.Errorf("error removing task: %v", err)
			}
		case lua.ResumeYield:
			if task.RaiseError != nil {
				task.thread.RaiseError(task.RaiseError.Error())
				_ = e.removeTask(task)
				return nil, fmt.Errorf("error in task: %v", task.RaiseError)
			} else {
				// Continue
				state, err, values = e.vm.state.Resume(task.thread, nil, task.Resumed...)
				if err != nil {
					_ = e.removeTask(task)
					return nil, fmt.Errorf("error resuming task: %v", err)
				}
			}
		default:
			return nil, fmt.Errorf("invalid task state: %v", task.State)
		}

		task.State = state
		task.Yielded = values
		task.Resumed = nil

		if state == lua.ResumeYield {
			yeildedTasks = append(yeildedTasks, task)
		}
	}

	return yeildedTasks, nil
}

func (e *CoroutineVM) GetYieldedTasks() []*Task {
	yielded := make([]*Task, 0)
	for _, task := range e.tasks {
		if task.State == lua.ResumeYield {
			yielded = append(yielded, task)
		}
	}
	return yielded
}

func (e *CoroutineVM) removeTask(task *Task) error {
	for i, t := range e.tasks {
		if t == task {
			if task.cancel != nil {
				task.cancel()
			}
			e.tasks = append(e.tasks[:i], e.tasks[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("task not found")
}

func (e *CoroutineVM) Close() {
	for _, task := range e.tasks {
		if task.cancel != nil {
			task.cancel()
		}
	}
	if e.vm != nil {
		e.vm.Close()
	}
}
