package engine

import (
	"container/list"
	"context"
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
	"go.uber.org/zap"
	"strings"
)

var coroOptions = []Option{WithGlobalValue("_COROUTINE_ENABLED", lua.LTrue)}

// Task represents a coroutine execution unit in the Lua VM.
// It maintains the state and context of a running coroutine.
type Task struct {
	l          *lua.LState
	thread     *lua.LState
	cancel     context.CancelFunc
	fn         *lua.LFunction
	output     chan Result
	State      lua.ResumeState
	Yielded    []lua.LValue
	Resumed    []lua.LValue
	RaiseError error
}

// Thread returns the Lua state associated with this task's coroutine.
func (t *Task) Thread() *lua.LState {
	return t.thread
}

// Type returns the Lua type of this task (LTThread).
func (t *Task) Type() lua.LValueType {
	return lua.LTThread
}

func (t *Task) String() string {
	return fmt.Sprintf("<coroutine %p> %+v", t.thread, t.Yielded)
}

// Result represents the outcome of a coroutine execution.
// It contains the final state, return values, and any error that occurred.
type Result struct {
	State  *lua.LState
	Result []lua.LValue
	Error  error
}

// TaskQueue manages a queue of coroutine tasks waiting for execution.
type TaskQueue struct {
	active *list.List
}

// NewTaskQueue creates and initializes a new TaskQueue instance.
func NewTaskQueue() *TaskQueue {
	return &TaskQueue{
		active: list.New(),
	}
}

// Push adds a new task to the end of the queue.
func (q *TaskQueue) Push(task *Task) {
	q.active.PushBack(task)
}

// Pop removes and returns the first task in the queue.
// Returns nil if the queue is empty.
func (q *TaskQueue) Pop() *Task {
	if q.active.Len() == 0 {
		return nil
	}
	e := q.active.Front()
	q.active.Remove(e)
	return e.Value.(*Task)
}

// Drain removes and returns all tasks from the queue.
func (q *TaskQueue) Drain() []*Task {
	tasks := make([]*Task, 0, q.active.Len())
	for e := q.active.Front(); e != nil; e = e.Next() {
		t := e.Value.(*Task)
		if t != nil {
			tasks = append(tasks, t)
		}
	}
	q.active.Init()
	return tasks
}

// IsEmpty returns true if the queue contains no tasks.
func (q *TaskQueue) IsEmpty() bool {
	return q.active.Len() == 0
}

// Len returns the number of tasks currently in the queue.
func (q *TaskQueue) Len() int {
	return q.active.Len()
}

// CoroutineVM represents a Lua virtual machine with coroutine support.
// This VM is NOT thread safe, external synchronization is required.
type CoroutineVM struct {
	vm    *VM
	tasks []*Task
	queue *TaskQueue
}

// IsCoroutineVM checks if the given Lua state has coroutine support enabled
// by verifying the presence of the _COROUTINE_ENABLED global variable.
func IsCoroutineVM(L *lua.LState) bool {
	//check _COROUTINE_ENABLED
	if L.GetGlobal("_COROUTINE_ENABLED") != lua.LTrue {
		return false
	}

	return true
}

// NewCVM creates a new CoroutineVM instance with the provided context, logger and options.
// Context is required for proper async operation and resource cleanup.
func NewCVM(
	log *zap.Logger,
	opts ...Option,
) (*CoroutineVM, error) {
	vm, err := NewVM(log, append(coroOptions, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	avm := &CoroutineVM{
		vm:    vm,
		tasks: make([]*Task, 0),
		queue: NewTaskQueue(),
	}
	avm.bindCoroutines()

	return avm, nil
}

// Import loads a script and stores its named functions
func (e *CoroutineVM) Import(s, name string, funcName ...string) error {
	if len(funcName) == 0 {
		return fmt.Errorf("no function names provided for export")
	}

	chunk, err := parse.Parse(strings.NewReader(s), name)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	fnProto, err := lua.Compile(chunk, name)
	if err != nil {
		return fmt.Errorf("compile error: %w", err)
	}

	return e.Mount(fnProto, funcName...)
}

// StartString starts a Lua script string with the given name and arguments as coroutine. Step is required to advance execution.
func (e *CoroutineVM) StartString(ctx context.Context, script, scriptName string, args ...lua.LValue) error {
	fn, err := e.vm.state.Load(strings.NewReader(script), scriptName)
	if err != nil {
		return err
	}

	task, err := e.createTask(ctx, fn)
	if err != nil {
		return err
	}
	task.Resumed = args

	return err
}

// Mount loads and mounts (executes) provided function(s) prototype.
// Use it to share CoroutineVM code between instances.
func (e *CoroutineVM) Mount(proto *lua.FunctionProto, funcName ...string) error {
	return e.vm.Mount(proto, funcName...)
}

// Start begins execution of a named function with the provided arguments.
func (e *CoroutineVM) Start(ctx context.Context, funcName string, args ...lua.LValue) (<-chan Result, error) {
	fn, ok := e.vm.exported[funcName]
	if !ok {
		return nil, fmt.Errorf("function %q not found", funcName)
	}

	if e.vm.state.Context() == nil {
		// probably think about alternative way
		e.vm.state.SetContext(ctx)
	}

	task, err := e.createTask(ctx, fn)
	if err != nil {
		return nil, fmt.Errorf("failed to create coroutine: %w", err)
	}
	task.Resumed = args
	task.output = make(chan Result, 1)

	return task.output, nil
}

// Step advances the execution of provided tasks or continues with queued tasks.
// Returns yielded tasks and any errors encountered during execution.
func (e *CoroutineVM) Step(tasks ...*Task) (result []*Task, finalErr error) {
	// Lua 5.1 does not allow yields as part of pcall, which can cause engine panic
	// We need to recover from it in case of user error
	// TODO: Properly isolate this issue and make upstream PR
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

	yieldedTasks := make([]*Task, 0)

	for !e.queue.IsEmpty() {
		task := e.queue.Pop()

		if task.State == lua.ResumeYield {
			if task.RaiseError != nil {
				if task.output != nil {
					task.output <- Result{State: task.l, Error: task.RaiseError}
					close(task.output)
					task.output = nil
				}
				_ = e.removeTask(task)
				return nil, task.RaiseError
			}

			state, err, values = e.vm.state.Resume(task.thread, task.fn, task.Resumed...)
			if err != nil {
				if task.output != nil {
					task.output <- Result{State: task.l, Error: err}
					close(task.output)
					task.output = nil
				}
				_ = e.removeTask(task)
				return nil, fmt.Errorf("error resuming task: %v", err)
			}

			task.State = state
			task.Yielded = values
			task.Resumed = nil
		}

		if state == lua.ResumeYield {
			yieldedTasks = append(yieldedTasks, task)
		} else if state == lua.ResumeOK || state == lua.ResumeError {
			if task.output != nil {
				if top := task.thread.GetTop(); top > 0 {
					task.output <- Result{State: task.l, Result: values}
				} else {
					task.output <- Result{State: task.l}
				}
				close(task.output)
				task.output = nil
			}
			_ = e.removeTask(task)
		}
	}

	return yieldedTasks, nil
}

// GetTasks returns all tasks running in VM.
func (e *CoroutineVM) GetTasks() []*Task {
	yielded := make([]*Task, 0)
	for _, task := range e.tasks {
		yielded = append(yielded, task)
	}

	return yielded
}

// GetTask retrieves a Task associated with the given Lua state.
func (e *CoroutineVM) GetTask(thread *lua.LState) (*Task, error) {
	for _, task := range e.tasks {
		if task.thread == thread {
			return task, nil
		}
	}
	return nil, fmt.Errorf("task not found")
}

// Close cleans up resources and terminates all running tasks.
func (e *CoroutineVM) Close() {
	for _, task := range e.tasks {
		if task.cancel != nil {
			task.cancel()
		}

		if task.output != nil {
			close(task.output)
			task.output = nil
		}
	}

	if e.vm != nil {
		e.vm.Close()
	}
}

// State returns the underlying Lua state of the VM.
func (e *CoroutineVM) State() *lua.LState {
	return e.vm.state
}

// bindCoroutines sets up coroutine-related functions in the Lua environment.
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

			task, err := e.createTask(L.Context(), fn)
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

// createTask creates a new coroutine task from a Lua function.
func (e *CoroutineVM) createTask(ctx context.Context, fn *lua.LFunction) (*Task, error) {
	thread, cancel := e.vm.state.NewThread()
	thread.SetContext(ctx)

	task := &Task{
		l:      e.vm.state,
		thread: thread,
		cancel: cancel,
		fn:     fn,
		State:  lua.ResumeYield,
	}

	e.tasks = append(e.tasks, task)
	e.queue.Push(task)

	return task, nil
}

// removeTask removes a task from the task list and performs cleanup.
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
