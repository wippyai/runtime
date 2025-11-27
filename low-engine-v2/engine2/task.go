package engine2

import (
	"fmt"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// Task pool for reuse
var taskPool = sync.Pool{
	New: func() interface{} {
		return &Task{
			retBuf:    make([]lua.LValue, 0, 8),
			resumeBuf: make([]lua.LValue, 0, 4),
		}
	},
}

// Task represents a coroutine execution unit in the Lua VM.
type Task struct {
	thread *lua.LState
	fn     *lua.LFunction

	State   lua.ResumeState
	Yielded []lua.LValue
	Resumed []lua.LValue

	// pcallFrom tracks parent task for cpcall error handling
	pcallFrom *lua.LState
	blocked   bool

	// retBuf is a reusable buffer for Resume return values
	retBuf []lua.LValue

	// resumeBuf is a reusable buffer for Resumed values
	resumeBuf []lua.LValue
}

// NewTask creates a task from an existing thread and function.
func NewTask(thread *lua.LState, fn *lua.LFunction) *Task {
	t := taskPool.Get().(*Task)
	t.thread = thread
	t.fn = fn
	t.State = lua.ResumeYield
	t.Yielded = nil
	t.Resumed = nil
	t.pcallFrom = nil
	t.blocked = false
	t.retBuf = t.retBuf[:0]
	return t
}

// Thread returns the Lua state associated with this task's coroutine.
func (t *Task) Thread() *lua.LState {
	return t.thread
}

// Function returns the Lua function being executed.
func (t *Task) Function() *lua.LFunction {
	return t.fn
}

// IsBlocked returns true if the task is blocked (e.g., waiting for cpcall).
func (t *Task) IsBlocked() bool {
	return t.blocked
}

// SetBlocked sets the blocked state.
func (t *Task) SetBlocked(blocked bool) {
	t.blocked = blocked
}

// PcallFrom returns the parent state if this task was spawned via cpcall.
func (t *Task) PcallFrom() *lua.LState {
	return t.pcallFrom
}

// SetPcallFrom sets the parent state for cpcall tracking.
func (t *Task) SetPcallFrom(state *lua.LState) {
	t.pcallFrom = state
}

// Type returns the Lua type of this task (LTThread).
func (t *Task) Type() lua.LValueType {
	return lua.LTThread
}

func (t *Task) String() string {
	return fmt.Sprintf("<coroutine %p>", t.thread)
}

// ResumeWith sets the Resumed slice using the internal buffer.
func (t *Task) ResumeWith(values ...lua.LValue) {
	t.resumeBuf = t.resumeBuf[:0]
	t.resumeBuf = append(t.resumeBuf, values...)
	t.Resumed = t.resumeBuf
}

// Close releases the task's resources and returns it to the pool.
func (t *Task) Close() {
	if t.thread != nil {
		t.thread.Close()
		t.thread = nil
	}
	t.fn = nil
	t.pcallFrom = nil
	t.Yielded = nil
	t.Resumed = nil
	t.blocked = false
	t.State = 0
	taskPool.Put(t)
}
