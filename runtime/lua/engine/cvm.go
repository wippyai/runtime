package engine

import (
	"container/list"
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ponyruntime/pony/runtime/lua/engine/errors"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
	"go.uber.org/zap"
)

// Task represents a coroutine execution unit in the Lua VM.
// It maintains the state and context of a running coroutine.
type Task struct {
	thread *lua.LState
	cancel context.CancelFunc
	fn     *lua.LFunction
	output chan *Update

	State      lua.ResumeState
	Yielded    []lua.LValue
	Resumed    []lua.LValue
	RaiseError error

	pcallFrom *lua.LState
	blocked   bool
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

// TaskQueue manages a queue of coroutine threads waiting for execution.
type TaskQueue struct {
	active *list.List
	mu     sync.RWMutex
}

// NewTaskQueue creates and initializes a new TaskQueue instance.
func NewTaskQueue() *TaskQueue {
	return &TaskQueue{
		active: list.New(),
	}
}

// Push adds a new task to the end of the queue.
func (q *TaskQueue) Push(task *Task) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.active.PushBack(task)
}

// Pop removes and returns the first task in the queue.
// Returns nil if the queue is empty.
func (q *TaskQueue) Pop() *Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.active.Len() == 0 {
		return nil
	}
	e := q.active.Front()
	q.active.Remove(e)

	return e.Value.(*Task)
}

// Drain removes and returns all threads from the queue.
func (q *TaskQueue) Drain() []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()

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

// IsEmpty returns true if the queue contains no threads.
func (q *TaskQueue) IsEmpty() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.active.Len() == 0
}

// Len returns the number of threads currently in the queue.
func (q *TaskQueue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.active.Len()
}

// CoroutineVM represents a Lua virtual machine with coroutine support.
// This VM is NOT thread safe, external synchronization is required.
type CoroutineVM struct {
	vm      *VM
	threads []*Task
	queue   *TaskQueue
}

// IsCoroutineVM checks if the given Lua state has coroutine support enabled
// by verifying the presence of the _COROUTINE_ENABLED global variable.
func IsCoroutineVM(l *lua.LState) bool {
	// check _COROUTINE_ENABLED
	return l.GetGlobal("_COROUTINE_ENABLED") == lua.LTrue
}

// NewCVM creates a new CoroutineVM instance with the provided context, logger, and options.
// Context is required for proper async operation and resource cleanup.
func NewCVM(
	log *zap.Logger,
	opts ...Option,
) (*CoroutineVM, error) {
	vm, err := NewVM(log, append([]Option{WithGlobalValue("_COROUTINE_ENABLED", lua.LTrue)}, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	avm := &CoroutineVM{
		vm:      vm,
		threads: make([]*Task, 0),
		queue:   NewTaskQueue(),
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

	e.vm.state.SetContext(ctx) // must be released by uow
	task := e.createTask(ctx, fn)
	task.Resumed = args

	return nil
}

// Mount loads and mounts (executes) provided function(s) prototype.
// Use it to share CoroutineVM code between instances.
func (e *CoroutineVM) Mount(proto *lua.FunctionProto, funcName ...string) error {
	return e.vm.Mount(proto, funcName...)
}

// Start begins execution of a named function with the provided arguments.
func (e *CoroutineVM) Start(ctx context.Context, funcName string, args ...lua.LValue) (<-chan *Update, error) {
	fn, ok := e.vm.exported[funcName]
	if !ok {
		return nil, fmt.Errorf("function %q not found", funcName)
	}

	e.vm.state.SetContext(ctx) // must be released by uow
	task := e.createTask(ctx, fn)
	task.Resumed = args
	task.output = make(chan *Update, 1)

	return task.output, nil
}

// Step advances the execution of provided threads or continues with queued threads.
// Returns yielded threads and any errors encountered during execution.
func (e *CoroutineVM) Step(tasks ...*Task) (result []*Task, finalErr error) {
	// Lua 5.1 does not allow yields as part of pcall, which can cause engine panic
	// We need to recover from it in case of user error, instead use cpcall
	defer func() {
		if r := recover(); r != nil {
			finalErr = fmt.Errorf("panic: %v", r)
		}
	}()

	// AddCleanup threads to queue
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
				var rErr error
				rErr = task.RaiseError

				if wrapped := errors.GetWrappedError(rErr); wrapped != nil {
					rErr = wrapped
				}

				if task.output != nil {
					task.output <- &Update{State: task.thread, Error: rErr}
					close(task.output)
					task.output = nil
				}

				_ = e.removeTask(task)
				return nil, rErr
			}

			state, err, values = e.vm.state.Resume(task.thread, task.fn, task.Resumed...)
			if err != nil {
				var rErr error
				rErr = err
				if wrapped := errors.GetWrappedError(rErr); wrapped != nil {
					rErr = wrapped
				}

				if task.output != nil {
					task.output <- &Update{State: task.thread, Error: rErr}
					close(task.output)
					task.output = nil
				}

				if task.pcallFrom != nil {
					t, cerr := e.GetTask(task.pcallFrom)
					if cerr != nil {
						_ = e.removeTask(task)
						return nil, cerr
					}
					_ = e.removeTask(task)

					// Resume the task that called pcall
					t.Resumed = []lua.LValue{lua.LFalse, errors.ToValue(t.thread, rErr)}
					t.blocked = false
					e.queue.Push(t)

					continue
				}

				_ = e.removeTask(task)
				return nil, rErr
			}

			task.State = state
			task.Yielded = values
			task.Resumed = nil

			if task.blocked {
				continue
			}
		}

		if state == lua.ResumeYield {
			yieldedTasks = append(yieldedTasks, task)
		} else if state == lua.ResumeOK || state == lua.ResumeError {
			if task.output != nil {
				if top := task.thread.GetTop(); top > 0 {
					task.output <- &Update{State: task.thread, Result: values}
				} else {
					task.output <- &Update{State: task.thread}
				}
				close(task.output)
				task.output = nil
			}

			// unfreeze parent and send it's result
			if task.pcallFrom != nil {
				if t, err := e.GetTask(task.pcallFrom); err == nil {
					t.Resumed = append([]lua.LValue{lua.LTrue}, values...)
					t.blocked = false
					e.queue.Push(t)
				}
				task.pcallFrom = nil
			}

			_ = e.removeTask(task)
		}
	}

	return yieldedTasks, nil
}

// GetTasks returns all threads running in VM.
func (e *CoroutineVM) GetTasks() []*Task {
	yielded := make([]*Task, 0)
	yielded = append(yielded, e.threads...)
	return yielded
}

// GetTask retrieves a Task associated with the given Lua state.
func (e *CoroutineVM) GetTask(thread *lua.LState) (*Task, error) {
	for _, task := range e.threads {
		if task.thread == thread {
			return task, nil
		}
	}

	return nil, fmt.Errorf("task not found")
}

// Close cleans up resources and terminates all running threads.
func (e *CoroutineVM) Close() {
	for _, task := range e.threads {
		task.thread.Close()
		task.fn = nil
		task.thread = nil

		if task.output != nil {
			close(task.output)
			task.output = nil
		}
	}

	if e.queue != nil {
		e.queue.Drain()
	}
	e.queue = nil
	e.threads = nil

	if e.vm != nil {
		e.vm.Close()
		e.vm = nil
	}
}

// State returns the underlying Lua state of the VM.
func (e *CoroutineVM) State() *lua.LState {
	return e.vm.state
}

// bindCoroutines sets up coroutine-related functions in the Lua environment.
func (e *CoroutineVM) bindCoroutines() {
	coTable := e.vm.state.GetGlobal("coroutine").(*lua.LTable)

	e.vm.state.SetField(coTable, "spawn", e.vm.state.NewFunction(func(l *lua.LState) int {
		fnValue := l.Get(1)

		if fnValue.Type() != lua.LTFunction {
			l.RaiseError("coroutine.spawn() requires a function argument")
			return 0
		}

		if fn, ok := fnValue.(*lua.LFunction); ok {
			if fn.IsG || len(fn.Upvalues) > 0 {
				for _, upval := range fn.Upvalues {
					if _, isThread := upval.Value().(*lua.LState); isThread {
						l.RaiseError("cannot spawn vm coroutines")
						return 0
					}
				}
			}

			task := e.createTask(l.Context(), fn)

			l.Push(task.thread)
			return 1
		}

		l.RaiseError("internal error: function cast failed")
		return 0
	}))

	// Modify resume to prevent resuming VM threads
	oldResume := coTable.RawGetString("resume").(*lua.LFunction)
	e.vm.state.SetField(coTable, "resume", e.vm.state.NewFunction(func(l *lua.LState) int {
		co := l.CheckThread(1)
		l.Push(oldResume)
		l.Push(co)
		l.Call(1, lua.MultRet)
		return l.GetTop() - 1
	}))

	e.vm.state.SetGlobal("cpcall", e.vm.state.NewFunction(func(l *lua.LState) int {
		// get func
		fn := l.Get(1)

		// get args
		args := make([]lua.LValue, l.GetTop()-1)
		for i := 2; i <= l.GetTop(); i++ {
			args[i-2] = l.Get(i)
		}

		// isolate into thread
		task := e.createTask(l.Context(), fn.(*lua.LFunction))
		task.Resumed = args
		task.pcallFrom = l

		t, err := e.GetTask(l)
		if err != nil {
			l.RaiseError("pcall: internal error")
			return 0
		}
		t.blocked = true

		return -1
	}))
}

// createTask creates a new coroutine task from a Lua function.
func (e *CoroutineVM) createTask(ctx context.Context, fn *lua.LFunction) *Task {
	thread, cancel := e.vm.state.NewThread()
	thread.SetContext(ctx)

	// todo: we can pool it actually
	task := &Task{
		thread: thread,
		cancel: cancel,
		fn:     fn,
		State:  lua.ResumeYield,
	}

	e.threads = append(e.threads, task)
	e.queue.Push(task)

	return task
}

// removeTask removes a task from the task list and performs cleanup.
func (e *CoroutineVM) removeTask(task *Task) error {
	for i, t := range e.threads {
		if t == task {
			task.thread.Close()
			task.fn = nil
			task.thread = nil
			task.pcallFrom = nil

			e.threads = append(e.threads[:i], e.threads[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("task not found")
}
