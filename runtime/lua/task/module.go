package task

import (
	"errors"

	luaconv "github.com/ponyruntime/pony/system/payload/lua"

	"github.com/ponyruntime/pony/runtime/lua/engine/value"

	"github.com/ponyruntime/pony/api/payload"
	lua "github.com/yuin/gopher-lua"
)

// Module represents the task2 Lua module
type Module struct{}

// NewTaskModule creates a new task2 module instance
func NewTaskModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "task"
}

// Loader registers the module functions
func (m *Module) Loader(l *lua.LState) int {
	value.RegisterMethods(l, "task.Task", map[string]lua.LGFunction{
		"input":    m.taskInput,
		"complete": m.taskComplete,
		"fail":     m.taskFail,
	})

	return 1
}

// taskInput implements task:input() -> returns task input value
func (m *Module) taskInput(l *lua.LState) int {
	task := CheckTask(l, 1)

	// Get transcoder directly from context
	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		l.RaiseError("no transcoder found in context")
		return 0
	}

	// Convert payload to Lua if needed
	if task.Input.Format() != payload.Lua {
		luaPayload, err := dtt.Transcode(task.Input, payload.Lua)
		if err != nil {
			l.RaiseError("failed to transcode input: %v", err)
			return 0
		}

		if lv, ok := luaPayload.Data().(lua.LValue); ok {
			l.Push(lv)
			return 1
		}
	} else if lv, ok := task.Input.Data().(lua.LValue); ok {
		l.Push(lv)
		return 1
	}

	l.RaiseError("invalid input payload format")
	return 0
}

// taskComplete implements task:complete(value) - completes task with given value
func (m *Module) taskComplete(l *lua.LState) int {
	task := CheckTask(l, 1)
	result := l.CheckAny(2)

	// Create result payload
	resultPayload := luaconv.ExportPayload(result)

	// Complete the task
	err := task.Complete(resultPayload)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

// taskFail implements task:fail(error) - fails task with error
func (m *Module) taskFail(l *lua.LState) int {
	task := CheckTask(l, 1)
	errMsg := l.CheckString(2)

	// Fail the task
	err := task.Fail(errors.New(errMsg))
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

// CheckTask validates and returns the task handle from Lua stack
func CheckTask(l *lua.LState, n int) *Task {
	ud := l.CheckUserData(n)
	if v, ok := ud.Value.(*Task); ok {
		return v
	}
	l.ArgError(n, "task expected")
	return nil
}

// WrapTask wraps a task in a Lua userdata
func WrapTask(l *lua.LState, task *Task) lua.LValue {
	ud := l.NewUserData()
	ud.Value = task
	ud.Metatable = value.GetTypeMetatable(l, "task.Task")

	return ud
}
