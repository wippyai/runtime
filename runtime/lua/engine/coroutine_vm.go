package engine

import (
	"container/list"
	"context"
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type Task struct {
	l             *lua.LState
	thread        *lua.LState
	state         lua.ResumeState
	yieldedVals   []lua.LValue
	resumeVal     lua.LValue
	cancel        context.CancelFunc
	yieldCycle    int
	lastYieldVals []lua.LValue // Last yielded values before polling
	fn            *lua.LFunction
}

func (t *Task) IsYielded() bool {
	return t.state == lua.ResumeYield
}

func (t *Task) GetYieldedValues() []lua.LValue {
	return t.yieldedVals
}

func (t *Task) SetResumeValue(val lua.LValue) {
	t.resumeVal = val
}

type taskQueue struct {
	active *list.List
}

func newTaskQueue() *taskQueue {
	return &taskQueue{
		active: list.New(),
	}
}

func (q *taskQueue) Push(task *Task) {
	q.active.PushBack(task)
}

func (q *taskQueue) Pop() *Task {
	if q.active.Len() == 0 {
		return nil
	}
	e := q.active.Front()
	q.active.Remove(e)
	return e.Value.(*Task)
}

func (q *taskQueue) IsEmpty() bool {
	return q.active.Len() == 0
}

type CoroutineVM struct {
	ctx        context.Context
	vm         *VM
	tasks      []*Task
	queue      *taskQueue
	chanCoord  *ChannelCoordinator
	yieldCycle int
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
		ctx:       ctx,
		vm:        vm,
		tasks:     make([]*Task, 0),
		queue:     newTaskQueue(),
		chanCoord: NewChannelCoordinator(),
	}
	avm.vm.state.SetContext(ctx)
	avm.bindCoroutines()
	avm.bindChannels()

	return avm, nil
}

func (e *CoroutineVM) DoString(script, name string) error {
	return e.vm.DoString(nil, script, name)
}

func (e *CoroutineVM) bindCoroutines() {
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
		fn:     fn,
		state:  -1,
	}

	e.tasks = append(e.tasks, task)
	e.queue.Push(task)

	return task, nil
}

func (e *CoroutineVM) Step(tasks ...*Task) ([]*Task, error) {
	// Add tasks to queue
	for _, t := range tasks {
		e.queue.Push(t)
	}

	e.yieldCycle++

	var state lua.ResumeState
	var err error
	var values []lua.LValue

	for !e.queue.IsEmpty() {
		task := e.queue.Pop()

		switch task.state {
		case -1:
			// Start
			state, err, values = e.vm.state.Resume(task.thread, task.fn)
			if err != nil {
				task.cancel()
				return nil, fmt.Errorf("error starting task: %v", err)
			}
		case lua.ResumeYield:
			// Continue
			state, err, values = e.vm.State().Resume(task.thread, nil, task.resumeVal)
			if err != nil {
				return nil, fmt.Errorf("error resuming task: %v", err)
			}
		default:
			return nil, fmt.Errorf("invalid task state: %v", task.state)
		}

		task.state = state
		task.yieldedVals = values
		task.resumeVal = nil

		if state == lua.ResumeYield {
			// We handle channel operations inside the VM
			if op := getChannelOp(task.yieldedVals); op != nil {
				for _, t := range e.chanCoord.PushOperation(task, op) {
					e.queue.Push(t)
				}
				continue
			}

			task.yieldCycle = e.yieldCycle
		}
	}

	// get all tasks that are pending on external yields after this cycle
	newlyYielded := make([]*Task, 0)
	for _, t := range e.tasks {
		if t.IsYielded() && t.yieldCycle == e.yieldCycle {
			newlyYielded = append(newlyYielded, t)
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

// Get first channel operation from yielded values if present
func getChannelOp(values []lua.LValue) *ChanOperation {
	if len(values) == 0 {
		return nil
	}

	if op, ok := values[0].(*ChanOperation); ok {
		return op
	}

	return nil
}
