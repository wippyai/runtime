package engine

import (
	"context"
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type Task struct {
	l           *lua.LState
	thread      *lua.LState
	state       lua.ResumeState
	yieldedVals []lua.LValue
	resumeVal   lua.LValue
	cancel      context.CancelFunc
	// Fields for condition monitoring
	condition     *lua.LFunction // Condition to check
	isPolling     bool           // Whether task is polling
	wasYielded    bool           // Whether task yielded before current step
	lastYieldVals []lua.LValue   // Last yielded values before polling
}

func (t *Task) IsYielded() bool {
	return t.state == lua.ResumeYield || t.isPolling
}

func (t *Task) GetYieldedValues() []lua.LValue {
	if t.isPolling {
		return t.lastYieldVals
	}
	return t.yieldedVals
}

func (t *Task) SetResumeValue(val lua.LValue) {
	t.resumeVal = val
}

type CoroutineVM struct {
	ctx   context.Context
	vm    *VM
	tasks []*Task
}

func NewCoroutineVM(
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
	}
	avm.vm.state.SetContext(ctx)
	avm.bindThreading()

	return avm, nil
}

func (e *CoroutineVM) DoString(script, name string) error {
	return e.vm.DoString(nil, script, name)
}

func (e *CoroutineVM) bindThreading() {
	coTable := e.vm.state.GetGlobal("coroutine").(*lua.LTable)

	// Add spawn function
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
						L.RaiseError("cannot spawn wrapped coroutines")
						return 0
					}
				}
			}
		}

		fn := fnValue.(*lua.LFunction)
		task, err := e.createCoroutine(fn)
		if err != nil {
			L.RaiseError("failed to spawn coroutine: %v", err)
			return 0
		}

		L.Push(task.thread)
		return 1
	}))

	// Add when function for condition monitoring
	e.vm.state.SetField(coTable, "wait", e.vm.state.NewFunction(func(L *lua.LState) int {
		condition := L.CheckFunction(1)

		// Get current task
		thread := L.Context().Value("thread").(*lua.LState)
		var currentTask *Task
		for _, task := range e.tasks {
			if task.thread == thread {
				currentTask = task
				break
			}
		}

		if currentTask == nil {
			L.RaiseError("coroutine.wait() can only be called from a spawned task")
			return 0
		}

		// Store condition and mark as polling
		currentTask.condition = condition
		currentTask.isPolling = true
		currentTask.wasYielded = currentTask.state == lua.ResumeYield
		currentTask.lastYieldVals = currentTask.yieldedVals

		// Return control to VM
		return L.Yield(lua.LString("polling"))
	}))

	// Modify resume to prevent resuming VM threads
	oldResume := coTable.RawGetString("resume").(*lua.LFunction)
	e.vm.state.SetField(coTable, "resume", e.vm.state.NewFunction(func(L *lua.LState) int {
		co := L.CheckThread(1)
		if co == L {
			L.RaiseError("attempt to resume VM thread")
			return 0
		}

		L.Push(oldResume)
		L.Push(co)
		L.Call(1, lua.MultRet)
		return L.GetTop() - 1
	}))
}

func (e *CoroutineVM) createCoroutine(fn *lua.LFunction) (*Task, error) {
	thread, cancel := e.vm.state.NewThread()

	task := &Task{
		l:      e.vm.State(),
		thread: thread,
		cancel: cancel,
	}

	state, err, values := e.vm.State().Resume(thread, fn)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("error starting task: %v", err)
	}

	task.state = state
	task.yieldedVals = values

	e.tasks = append(e.tasks, task)
	return task, nil
}

func (e *CoroutineVM) Step(tasks ...*Task) ([]*Task, error) {
	newlyYielded := make([]*Task, 0)

	for _, task := range tasks {
		// Skip if not yielded or polling
		if task.state != lua.ResumeYield && !task.isPolling {
			continue
		}

		if task.isPolling {
			// Check condition
			e.vm.state.Push(task.condition)
			e.vm.state.Call(0, 1)
			conditionMet := e.vm.state.ToBool(-1)
			e.vm.state.Pop(1)

			if conditionMet {
				// Condition met, resume task
				task.isPolling = false
				task.condition = nil
				// If task was yielded before polling, keep it in yielded state
				if !task.wasYielded {
					task.state = lua.ResumeOK
					continue
				}
			} else {
				// Keep polling, add to yielded if it was yielded before
				if task.wasYielded {
					newlyYielded = append(newlyYielded, task)
				}
				continue
			}
		}

		// Resume task
		state, err, values := e.vm.State().Resume(task.thread, nil, task.resumeVal)
		if err != nil {
			return nil, fmt.Errorf("error resuming task: %v", err)
		}

		task.state = state
		task.yieldedVals = values
		task.resumeVal = nil

		// Add to newly yielded tasks if it yielded
		if state == lua.ResumeYield {
			newlyYielded = append(newlyYielded, task)
		}
	}

	return newlyYielded, nil
}

func (e *CoroutineVM) GetYieldedTasks() []*Task {
	yielded := make([]*Task, 0)
	for _, task := range e.tasks {
		if task.IsYielded() {
			yielded = append(yielded, task)
		}
	}
	return yielded
}

func (e *CoroutineVM) RemoveTask(task *Task) error {
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

func (e *CoroutineVM) Close() error {
	for _, task := range e.tasks {
		if task.cancel != nil {
			task.cancel()
		}
	}
	if e.vm != nil {
		e.vm.Close()
	}
	return nil
}
