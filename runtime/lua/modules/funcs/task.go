package funcs

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

var (
	// ErrTaskCanceled is returned when a task has been canceled
	ErrTaskCanceled = errors.New("task canceled")
)

// Task represents a function execution with cancellation capability
type Task struct {
	ctx        context.Context
	cancel     context.CancelFunc
	completed  bool
	error      error
	response   *channel.Channel // Channel for responding
	chValue    lua.LValue       // Lua channel value representation
	resultData lua.LValue
	isCanceled bool
	mu         sync.Mutex
}

// NewTask creates a new task with a cancellable context
func NewTask(l *lua.LState, parentCtx context.Context) *Task {
	ctx, cancel := context.WithCancel(parentCtx)

	// Create a named channel similar to Command
	channelName := fmt.Sprintf("task.%p", &ctx)
	ch := channel.Named(channelName, 1)
	chValue := channel.Wrap(l, ch)

	return &Task{
		ctx:        ctx,
		cancel:     cancel,
		completed:  false,
		error:      nil,
		response:   ch,
		chValue:    chValue,
		resultData: lua.LNil,
		isCanceled: false,
	}
}

// Context returns the task's context
func (t *Task) Context() context.Context {
	return t.ctx
}

// Cancel cancels the task execution and sets the standard canceled error
func (t *Task) Cancel() {
	t.mu.Lock()
	t.isCanceled = true
	t.mu.Unlock()

	t.SetError(ErrTaskCanceled)
	t.cancel()
}

// SetResult marks the task as completed with a result
func (t *Task) SetResult(result lua.LValue) {
	t.mu.Lock()
	t.resultData = result
	t.completed = true
	t.error = nil
	t.mu.Unlock()
}

// SetError marks the task as failed with an error
func (t *Task) SetError(err error) {
	t.mu.Lock()
	t.error = err
	t.completed = true
	t.mu.Unlock()
}

// RegisterTask registers the task type in the Lua state
func RegisterTask(l *lua.LState) *lua.LTable {
	mt := l.NewTypeMetatable("function.Task")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"response":    responseChannel,
		"error":       getTaskError,
		"is_complete": isTaskComplete,
		"result":      getTaskResult,
		"is_canceled": isTaskCanceled,
		"cancel":      cancelTask,
	}))
	return mt
}

// CheckTask extracts a Task from Lua userdata and validates it
func CheckTask(l *lua.LState, index int) *Task {
	ud := l.CheckUserData(index)
	if task, ok := ud.Value.(*Task); ok {
		return task
	}
	l.ArgError(index, "function.Task expected")
	return nil
}

// responseChannel is the Lua method for getting the response channel
func responseChannel(l *lua.LState) int {
	task := CheckTask(l, 1)
	if task == nil {
		return 0
	}

	l.Push(task.chValue)
	return 1
}

// cancelTask is the Lua method for canceling a task
func cancelTask(l *lua.LState) int {
	task := CheckTask(l, 1)
	if task == nil {
		return 0
	}

	task.Cancel()
	l.Push(lua.LBool(true))
	return 1
}

// isTaskComplete is the Lua method to check if a task is complete
func isTaskComplete(l *lua.LState) int {
	task := CheckTask(l, 1)
	if task == nil {
		return 0
	}

	task.mu.Lock()
	completed := task.completed
	task.mu.Unlock()

	l.Push(lua.LBool(completed))
	return 1
}

// getTaskError is the Lua method to get any error from a task
func getTaskError(l *lua.LState) int {
	task := CheckTask(l, 1)
	if task == nil {
		return 0
	}

	task.mu.Lock()
	err := task.error
	task.mu.Unlock()

	if err != nil {
		l.Push(lua.LString(err.Error()))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

// getTaskResult is the Lua method to get the result of a task
func getTaskResult(l *lua.LState) int {
	task := CheckTask(l, 1)
	if task == nil {
		return 0
	}

	task.mu.Lock()
	completed := task.completed
	result := task.resultData
	err := task.error
	task.mu.Unlock()

	if !completed {
		l.Push(lua.LNil)
		l.Push(lua.LString("task not completed"))
		return 2
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

// isTaskCanceled is the Lua method to check if a task was canceled
func isTaskCanceled(l *lua.LState) int {
	task := CheckTask(l, 1)
	if task == nil {
		return 0
	}

	task.mu.Lock()
	canceled := task.isCanceled
	task.mu.Unlock()

	l.Push(lua.LBool(canceled))
	return 1
}
