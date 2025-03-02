package task

import (
	"errors"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
)

// Task represents a task that can be executed and responded to
type Task struct {
	Input    lua.LValue
	Response chan any
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

	uw := uow.FromContext(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work")
		return 0
	}

	coroutine.Wrap(l, func() *engine.Update {
		select {
		case task.Response <- value:
			close(task.Response)
			return engine.NewUpdate(l, []lua.LValue{lua.LTrue}, nil)
		case <-uw.Context().Done():
			return engine.NewUpdate(l, nil, uw.Context().Err())
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

	uw := uow.FromContext(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work")
		return 0
	}

	coroutine.Wrap(l, func() *engine.Update {
		select {
		case task.Response <- value:
			return engine.NewUpdate(l, []lua.LValue{lua.LTrue}, nil)
		case <-uw.Context().Done():
			return engine.NewUpdate(l, nil, uw.Context().Err())
		}
	})

	return -1 // yield to scheduler
}

// taskFail implements task:fail(error) - fails task with error
func (m *Module) taskFail(l *lua.LState) int {
	task := CheckTask(l, 1)
	errMsg := l.CheckString(2)

	uw := uow.FromContext(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work")
		return 0
	}

	coroutine.Wrap(l, func() *engine.Update {
		select {
		case task.Response <- errors.New(errMsg):
			close(task.Response)
			return engine.NewUpdate(l, []lua.LValue{lua.LTrue}, nil)
		case <-uw.Context().Done():
			return engine.NewUpdate(l, nil, uw.Context().Err())
		}
	})

	return -1 // yield to scheduler
}

// taskInput implements task:input() -> returns task input values
func (m *Module) taskInput(l *lua.LState) int {
	task := CheckTask(l, 1)

	l.Push(task.Input)
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

// CreateTask creates a new task with payload-based input
func CreateTask(input lua.LValue) (*Task, error) {
	task := &Task{
		Input:    input,
		Response: make(chan any, 1),
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
