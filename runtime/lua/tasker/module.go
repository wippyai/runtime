package tasker

import (
	"errors"
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
	return "tasks.task." + string(t.id)
}

func (t *taskSchedule) Type() lua.LValueType {
	return lua.LTUserData
}

// Module represents tasks Lua module
type Module struct {
}

// NewModule creates a new tasks module instance
func NewModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "tasks"
}

// Loader registers the module functions
func (m *Module) Loader(L *lua.LState) int {
	// Create module table
	mod := L.NewTable()

	// Register functions
	L.SetField(mod, "channel", L.NewFunction(channelFunc))

	// Register task methods
	mt := L.NewTypeMetatable("tasks.Task")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
		"input":    m.taskInput,
		"complete": m.taskComplete,
		"fail":     m.taskFail,
		"send":     m.taskSend,
	}))

	// Register module
	L.Push(mod)
	return 1
}

func newTask(L *lua.LState, schedule *taskSchedule) lua.LValue {
	ud := L.NewUserData()
	ud.Value = schedule
	L.SetMetatable(ud, L.GetTypeMetatable("tasks.Task"))

	return ud
}

// channelFunc implements tasks.channel(buffer_size) -> yields channel request
func channelFunc(L *lua.LState) int {
	// Get buffer size with default of 1
	bufferSize := L.OptInt(1, 1)
	if bufferSize < 0 {
		L.RaiseError("buffer size must be >= 0")
		return 0
	}

	// Create and yield channel request
	L.Push(&channelRequest{bufferSize: bufferSize})
	return -1 // yield to scheduler
}

// taskComplete implements task:complete(values...)
func (m *Module) taskComplete(L *lua.LState) int {
	handle := checkTask(L)

	// Collect all values
	values := make([]lua.LValue, 0, L.GetTop()-1)
	for i := 2; i <= L.GetTop(); i++ {
		values = append(values, L.Get(i))
	}

	// send completion result directly to task channel
	handle.channel <- engine.Result{Result: values}
	close(handle.channel) // Close channel after completion
	return 0
}

// taskFail implements task:fail(error)
func (m *Module) taskFail(L *lua.LState) int {
	handle := checkTask(L)
	errMsg := L.CheckString(2)

	// send error result directly to task channel
	handle.channel <- engine.Result{Error: errors.New(errMsg)}
	close(handle.channel) // Close channel after failure
	return 0
}

// taskSend implements task:send(values...)
func (m *Module) taskSend(L *lua.LState) int {
	handle := checkTask(L)

	// Collect all values
	values := make([]lua.LValue, 0, L.GetTop()-1)
	for i := 2; i <= L.GetTop(); i++ {
		values = append(values, L.Get(i))
	}

	// send values directly to task channel
	handle.channel <- engine.Result{Result: values}
	return 0
}

// taskInput implements task:value() -> values...
func (m *Module) taskInput(L *lua.LState) int {
	handle := checkTask(L)

	// Push input values to stack
	for _, v := range handle.input {
		L.Push(v)
	}

	return len(handle.input)
}

// checkTask validates and returns the task handle from Lua stack
func checkTask(L *lua.LState) *taskSchedule {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*taskSchedule); ok {
		return v
	}
	L.ArgError(1, "task handle expected")
	return nil
}

// isChannelRequest checks if the value is our custom channel request
func isChannelRequest(v lua.LValue) (*channelRequest, bool) {
	if req, ok := v.(*channelRequest); ok {
		return req, true
	}
	return nil, false
}
