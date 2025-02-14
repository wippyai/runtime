// tasks.go
package tasks

import (
	"errors"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
)

// Task represents a task that can be executed and responded to
type Task struct {
	Input    payload.Payload
	Response chan payload.Payload
}

func (t *Task) String() string {
	return "tasks.Task"
}

func (t *Task) Type() lua.LValueType {
	return lua.LTUserData
}

// Module represents tasks Lua module
type Module struct{}

// NewTaskModule creates a new tasks module instance
func NewTaskModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "tasks"
}

// Loader registers the module functions
func (m *Module) Loader(l *lua.LState) int {
	mt := l.NewTypeMetatable("tasks.Task")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"input":    m.taskInput,
		"complete": m.taskComplete,
		"fail":     m.taskFail,
		"send":     m.taskSend,
	}))
	return 0
}

// taskComplete implements task:complete(value) - completes task with given value
func (m *Module) taskComplete(l *lua.LState) int {
	task := CheckTask(l, 1)
	value := l.CheckAny(2)

	coroutine.Wrap(l, func() *engine.Result {
		select {
		case task.Response <- payload.NewPayload(value, payload.Lua):
			close(task.Response)
			return engine.NewResult(l, []lua.LValue{lua.LTrue}, nil)
		case <-l.Context().Done():
			return engine.NewResult(l, nil, l.Context().Err())
		}
	})

	return -1 // yield to scheduler
}

// taskSend implements task:send(value) - sends intermediate value to task caller
func (m *Module) taskSend(l *lua.LState) int {
	task := CheckTask(l, 1)
	value := l.CheckAny(2)

	values := make([]lua.LValue, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		values = append(values, l.Get(i))
	}

	coroutine.Wrap(l, func() *engine.Result {
		select {
		case task.Response <- payload.NewPayload(value, payload.Lua):
			return engine.NewResult(l, []lua.LValue{lua.LTrue}, nil)
		case <-l.Context().Done():
			return engine.NewResult(l, nil, l.Context().Err())
		}
	})

	return -1 // yield to scheduler
}

// taskFail implements task:fail(error) - fails task with error
func (m *Module) taskFail(l *lua.LState) int {
	task := CheckTask(l, 1)
	errMsg := l.CheckString(2)

	coroutine.Wrap(l, func() *engine.Result {
		select {
		case task.Response <- payload.NewError(errors.New(errMsg)):
			close(task.Response)
			return engine.NewResult(l, []lua.LValue{lua.LTrue}, nil)
		case <-l.Context().Done():
			return engine.NewResult(l, nil, l.Context().Err())
		}
	})

	return -1 // yield to scheduler
}

// taskInput implements task:input() -> returns task input values
func (m *Module) taskInput(l *lua.LState) int {
	task := CheckTask(l, 1)

	tr := payload.GetTranscoder(l.Context())
	if tr == nil {
		l.RaiseError("no transcoder in context")
		return 0
	}

	luaPayload, err := tr.Transcode(task.Input, payload.Lua)
	if err != nil {
		l.RaiseError("failed to transcode input: %v", err)
		return 0
	}

	if luaValue, ok := luaPayload.Data().(lua.LValue); ok {
		l.Push(luaValue)
		return 1
	}

	l.RaiseError("invalid payload data type")
	return 0
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

// CreateTask creates a new task with payload-based input
func CreateTask(input payload.Payload) (*Task, error) {
	task := &Task{
		Input:    input,
		Response: make(chan payload.Payload, 1),
	}
	return task, nil
}

// WrapTask wraps a task in a Lua userdata
func WrapTask(l *lua.LState, task *Task) lua.LValue {
	ud := l.NewUserData()
	ud.Value = task
	l.SetMetatable(ud, l.GetTypeMetatable("tasks.Task"))

	return ud
}
