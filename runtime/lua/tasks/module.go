package tasks

import (
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// channelRequest represents our custom yield type for channel requests
type channelRequest struct {
	bufferSize int
}

func (r *channelRequest) String() string {
	return "tasks.channelRequest"
}

func (r *channelRequest) Type() lua.LValueType {
	return lua.LTUserData
}

func (t *taskSchedule) String() string {
	return fmt.Sprintf("tasks.task.%s", t.id)
}

func (t *taskSchedule) Type() lua.LValueType {
	return lua.LTUserData
}

// Module represents tasks Lua module
type Module struct {
}

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
	// Create module table
	mod := l.NewTable()

	// Register functions
	l.SetField(mod, "channel", l.NewFunction(channelFunc))

	// Register task methods
	mt := l.NewTypeMetatable("tasks.Task")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"input":    m.taskInput,
		"complete": m.taskComplete,
		"fail":     m.taskFail,
		"send":     m.taskSend,
	}))

	// Register module
	l.Push(mod)
	return 1
}

func newTask(l *lua.LState, schedule *taskSchedule) lua.LValue {
	ud := l.NewUserData()
	ud.Value = schedule
	l.SetMetatable(ud, l.GetTypeMetatable("tasks.Task"))

	return ud
}

// channelFunc implements tasks.channel(buffer_size) -> yields channel request
func channelFunc(l *lua.LState) int {
	// Get buffer size with a default of 1
	bufferSize := l.OptInt(1, 1)
	if bufferSize < 0 {
		l.RaiseError("buffer size must be >= 0")
		return 0
	}

	// Create and yield channel request
	l.Push(&channelRequest{bufferSize: bufferSize})
	return -1 // yield to scheduler
}

// taskComplete implements task:complete(values...)
func (m *Module) taskComplete(l *lua.LState) int {
	handle := checkTask(l)

	// Collect all values
	values := make([]lua.LValue, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		values = append(values, l.Get(i))
	}

	// send completion result directly to task channel
	handle.channel <- engine.Result{State: l, Result: values}
	close(handle.channel) // closeChannel channel after completion
	return 0
}

// taskFail implements task:fail(error)
func (m *Module) taskFail(l *lua.LState) int {
	handle := checkTask(l)
	errMsg := l.CheckString(2)

	// send error result directly to task channel
	handle.channel <- engine.Result{State: l, Error: errors.New(errMsg)}
	close(handle.channel) // closeChannel channel after failure
	return 0
}

// taskSend implements task:send(values...)
func (m *Module) taskSend(l *lua.LState) int {
	handle := checkTask(l)

	// Collect all values
	values := make([]lua.LValue, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		values = append(values, l.Get(i))
	}

	// send values directly to the task channel
	handle.channel <- engine.Result{State: l, Result: values}
	return 0
}

// taskInput implements task:value() -> values...
func (m *Module) taskInput(l *lua.LState) int {
	handle := checkTask(l)

	// Push input values to stack
	for _, v := range handle.input {
		l.Push(v)
	}

	return len(handle.input)
}

// checkTask validates and returns the task handle from Lua stack
func checkTask(l *lua.LState) *taskSchedule {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*taskSchedule); ok {
		return v
	}
	l.ArgError(1, "task handle expected")
	return nil
}

// isChannelRequest checks if the value is our custom channel request
func isChannelRequest(v lua.LValue) (*channelRequest, bool) {
	if req, ok := v.(*channelRequest); ok {
		return req, true
	}
	return nil, false
}
