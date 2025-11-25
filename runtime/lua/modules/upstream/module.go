package upstream

import (
	"errors"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/workflow/std"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// Module provides functionality to send values upstream from Lua
type Module struct {
	once        sync.Once
	moduleTable *lua.LTable
}

// NewUpstreamModule creates a new upstream module instance
func NewUpstreamModule() *Module {
	return &Module{}
}

func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "upstream",
		Description: "Upstream communication for workflow commands",
		Class:       []string{luaapi.ClassProcess},
	}
}

// Loader registers the module functions and metatables
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		value.RegisterMethods(l, "upstream.Request", map[string]lua.LGFunction{
			"response":    m.requestResponse,
			"type":        m.requestType,
			"complete":    m.requestComplete,
			"is_canceled": m.requestIsCanceled,
			"is_complete": m.requestIsComplete,
			"result":      m.requestResult,
			"cancel":      m.requestCancel,
		})

		value.RegisterMethods(l, "upstream.Task", map[string]lua.LGFunction{
			"type":     m.taskType,
			"input":    m.taskInput,
			"complete": m.taskComplete,
			"fail":     m.taskFail,
		})

		mod := l.NewTable()
		l.SetField(mod, "send", l.NewFunction(m.send))
		l.SetField(mod, "request", l.NewFunction(m.request))
		mod.Immutable = true
		m.moduleTable = mod
	})
	l.Push(m.moduleTable)
	return 1
}

// GetUpstreamSender retrieves the upstream sender from context using API
func GetUpstreamSender(l *lua.LState) (runtime.UpstreamSender, error) {
	ctx := l.Context()
	if ctx == nil {
		return nil, fmt.Errorf("no context found")
	}

	sender, ok := runtime.GetUpstreamSender(ctx)
	if !ok {
		return nil, fmt.Errorf("no upstream sender found in context")
	}

	return sender, nil
}

// send implements upstream.send(value)
// Accepts Request, Task, or regular payload
func (m *Module) send(l *lua.LState) int {
	val := l.CheckAny(1)

	// Check if it's a Request
	if ud, ok := val.(*lua.LUserData); ok {
		if req, ok := ud.Value.(*Request); ok {
			return m.sendRequest(l, req)
		}
		// Task can also be sent, but we just extract first input
		if task, ok := ud.Value.(*Task); ok {
			inputs := task.Input()
			if len(inputs) > 0 {
				return m.sendPayload(l, inputs[0])
			}
			l.RaiseError("task has no input")
			return 0
		}
	}

	// Regular payload - convert and send via channel
	p := luaconv.ExportPayload(val)
	return m.sendPayload(l, p)
}

// sendRequest handles Request objects
func (m *Module) sendRequest(l *lua.LState, req *Request) int {
	// Try to get Upstream handler (for workflows) from API
	upstream, ok := runtime.GetUpstream(l.Context())
	if !ok {
		// No Upstream handler - fall back to fire-and-forget via channel
		l.Push(lua.LFalse)
		l.Push(lua.LString("no upstream handler found in context"))
		return 2
	}

	// Queue request
	if err := upstream.SendRequest(req); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// sendPayload handles regular payloads via sender interface
func (m *Module) sendPayload(l *lua.LState, p payload.Payload) int {
	sender, err := GetUpstreamSender(l)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := sender.Send(p); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// request implements upstream.request(type, ...)
// Creates a new Request and auto-sends it to upstream handler (for workflows)
func (m *Module) request(l *lua.LState) int {
	reqType := l.CheckString(1)

	// Collect parameters
	var params []payload.Payload
	top := l.GetTop()
	for i := 2; i <= top; i++ {
		p := luaconv.ExportPayload(l.Get(i))
		params = append(params, p)
	}

	// Create request
	req := NewRequest(l, reqType, nil, params...)
	if req == nil {
		// Error already raised by NewRequest
		return 0
	}

	// Auto-send request to upstream handler (workflow pattern)
	// Workflows are synchronous/declarative, so request() should queue immediately
	upstream, ok := runtime.GetUpstream(l.Context())
	if ok {
		if err := upstream.SendRequest(req); err != nil {
			l.RaiseError("failed to send request: %v", err)
			return 0
		}
	}

	// Wrap in userdata
	ud := l.NewUserData()
	ud.Value = req
	ud.Metatable = value.GetTypeMetatable(l, "upstream.Request")

	l.Push(ud)
	return 1
}

// requestResponse implements request:response() -> channel
func (m *Module) requestResponse(l *lua.LState) int {
	req := CheckRequest(l, 1)

	// Return the response channel
	l.Push(req.channelValue)
	return 1
}

// requestType implements request:type() -> string
func (m *Module) requestType(l *lua.LState) int {
	req := CheckRequest(l, 1)

	l.Push(lua.LString(req.Type()))
	return 1
}

// requestComplete implements request:complete(result) -> nil
func (m *Module) requestComplete(l *lua.LState) int {
	req := CheckRequest(l, 1)
	resultValue := l.Get(2)

	// Convert Lua value to payload and create Result
	result := &runtime.Result{
		Value: payload.NewPayload(resultValue, payload.Lua),
		Error: nil,
	}

	err := req.Complete(result)
	if err != nil {
		l.RaiseError("failed to complete request: %v", err)
		return 0
	}

	return 0
}

// requestIsCanceled implements request:is_canceled() -> boolean
func (m *Module) requestIsCanceled(l *lua.LState) int {
	req := CheckRequest(l, 1)
	l.Push(lua.LBool(req.IsCanceled()))
	return 1
}

// requestIsComplete implements request:is_complete() -> boolean
func (m *Module) requestIsComplete(l *lua.LState) int {
	req := CheckRequest(l, 1)
	l.Push(lua.LBool(req.IsCompleted()))
	return 1
}

// requestResult implements request:result() -> (payload, error)
func (m *Module) requestResult(l *lua.LState) int {
	req := CheckRequest(l, 1)
	result := req.Result()

	if result == nil {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	if result.Error != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(result.Error.Error()))
		return 2
	}

	if result.Value != nil {
		l.Push(payloadmod.WrapPayload(l, result.Value))
		l.Push(lua.LNil)
		return 2
	}

	l.Push(lua.LNil)
	l.Push(lua.LNil)
	return 2
}

// requestCancel implements request:cancel() -> error
func (m *Module) requestCancel(l *lua.LState) int {
	req := CheckRequest(l, 1)

	err := req.Cancel()
	if err != nil {
		l.Push(lua.LString(err.Error()))
		return 1
	}

	l.Push(lua.LNil)
	return 1
}

// taskType implements task:type() -> returns task type string
func (m *Module) taskType(l *lua.LState) int {
	task := CheckTask(l, 1)
	l.Push(lua.LString(task.Type()))
	return 1
}

// taskInput implements task:input() -> returns first input value
func (m *Module) taskInput(l *lua.LState) int {
	task := CheckTask(l, 1)
	inputs := task.Input()

	if len(inputs) == 0 {
		l.Push(lua.LNil)
		return 1
	}

	// Return first input payload
	input := inputs[0]

	// If already Lua format, return directly
	if input.Format() == payload.Lua {
		if lv, ok := input.Data().(lua.LValue); ok {
			l.Push(lv)
			return 1
		}
	}

	// Need to transcode
	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		l.RaiseError("no transcoder found in context")
		return 0
	}

	luaPayload, err := dtt.Transcode(input, payload.Lua)
	if err != nil {
		l.RaiseError("failed to transcode input: %v", err)
		return 0
	}

	if lv, ok := luaPayload.Data().(lua.LValue); ok {
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

// CheckRequest validates and returns the Request from Lua stack
func CheckRequest(l *lua.LState, n int) *Request {
	ud := l.CheckUserData(n)
	if v, ok := ud.Value.(*Request); ok {
		return v
	}
	l.ArgError(n, "request expected")
	return nil
}

// WrapRequest wraps a Request in a Lua userdata
func WrapRequest(l *lua.LState, req *Request) lua.LValue {
	ud := l.NewUserData()
	ud.Value = req
	ud.Metatable = value.GetTypeMetatable(l, "upstream.Request")

	return ud
}

// CheckTask validates and returns the Task from Lua stack
func CheckTask(l *lua.LState, n int) std.Task {
	ud := l.CheckUserData(n)
	if v, ok := ud.Value.(std.Task); ok {
		return v
	}
	l.ArgError(n, "task expected")
	return nil
}

// WrapTask wraps a std.Task in a Lua userdata
func WrapTask(l *lua.LState, task std.Task) lua.LValue {
	ud := l.NewUserData()
	ud.Value = task
	ud.Metatable = value.GetTypeMetatable(l, "upstream.Task")

	return ud
}
