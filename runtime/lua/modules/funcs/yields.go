package funcs

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	funcapi "github.com/wippyai/runtime/api/dispatcher/func"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// anyToLua converts Go values to Lua values.
func anyToLua(l *lua.LState, v any) lua.LValue {
	if v == nil {
		return lua.LNil
	}
	switch val := v.(type) {
	case lua.LValue:
		return val
	case payload.Payload:
		return engine.PayloadToLua(l, val)
	case string:
		return lua.LString(val)
	case []byte:
		return lua.LString(val)
	case int:
		return lua.LNumber(val)
	case int64:
		return lua.LNumber(val)
	case float64:
		return lua.LNumber(val)
	case bool:
		return lua.LBool(val)
	case error:
		return lua.LString(val.Error())
	default:
		return lua.LString(fmt.Sprintf("%v", val))
	}
}

// CallYield wraps CallCmd for Lua.
type CallYield struct {
	*funcapi.CallCmd
}

var callYieldPool = sync.Pool{New: func() any { return &CallYield{} }}

func AcquireCallYield() *CallYield {
	y := callYieldPool.Get().(*CallYield)
	y.CallCmd = funcapi.AcquireCallCmd()
	return y
}

func ReleaseCallYield(y *CallYield) {
	if y.CallCmd != nil {
		y.CallCmd.Release()
		y.CallCmd = nil
	}
	callYieldPool.Put(y)
}

func (y *CallYield) String() string                { return "<func_call_yield>" }
func (y *CallYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *CallYield) ToCommand() dispatcher.Command { return y.CallCmd }
func (y *CallYield) CmdID() dispatcher.CommandID   { return funcapi.CmdCall }
func (y *CallYield) Release()                      { ReleaseCallYield(y) }

// HandleResult converts function call response to Lua values.
func (y *CallYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(funcapi.Response)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{anyToLua(l, resp.Value), lua.LNil}
}

// AsyncStartYield wraps AsyncStartCmd for Lua.
type AsyncStartYield struct {
	*funcapi.AsyncStartCmd
}

var asyncStartYieldPool = sync.Pool{New: func() any { return &AsyncStartYield{} }}

func AcquireAsyncStartYield() *AsyncStartYield {
	y := asyncStartYieldPool.Get().(*AsyncStartYield)
	y.AsyncStartCmd = funcapi.AcquireAsyncStartCmd()
	return y
}

func ReleaseAsyncStartYield(y *AsyncStartYield) {
	if y.AsyncStartCmd != nil {
		y.AsyncStartCmd.Release()
		y.AsyncStartCmd = nil
	}
	asyncStartYieldPool.Put(y)
}

func (y *AsyncStartYield) String() string                { return "<func_async_start_yield>" }
func (y *AsyncStartYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *AsyncStartYield) ToCommand() dispatcher.Command { return y.AsyncStartCmd }
func (y *AsyncStartYield) CmdID() dispatcher.CommandID   { return funcapi.CmdAsyncStart }
func (y *AsyncStartYield) Release()                      { ReleaseAsyncStartYield(y) }

// HandleResult converts async start response to Lua values.
func (y *AsyncStartYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(funcapi.AsyncStartResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{lua.LNumber(resp.CallID), lua.LNil}
}

// AsyncAwaitYield wraps AsyncAwaitCmd for Lua.
type AsyncAwaitYield struct {
	*funcapi.AsyncAwaitCmd
}

var asyncAwaitYieldPool = sync.Pool{New: func() any { return &AsyncAwaitYield{} }}

func AcquireAsyncAwaitYield() *AsyncAwaitYield {
	y := asyncAwaitYieldPool.Get().(*AsyncAwaitYield)
	y.AsyncAwaitCmd = funcapi.AcquireAsyncAwaitCmd()
	return y
}

func ReleaseAsyncAwaitYield(y *AsyncAwaitYield) {
	if y.AsyncAwaitCmd != nil {
		y.AsyncAwaitCmd.Release()
		y.AsyncAwaitCmd = nil
	}
	asyncAwaitYieldPool.Put(y)
}

func (y *AsyncAwaitYield) String() string                { return "<func_async_await_yield>" }
func (y *AsyncAwaitYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *AsyncAwaitYield) ToCommand() dispatcher.Command { return y.AsyncAwaitCmd }
func (y *AsyncAwaitYield) CmdID() dispatcher.CommandID   { return funcapi.CmdAsyncAwait }
func (y *AsyncAwaitYield) Release()                      { ReleaseAsyncAwaitYield(y) }

// HandleResult converts async await response to Lua values.
func (y *AsyncAwaitYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(funcapi.AsyncAwaitResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Cancelled {
		return []lua.LValue{lua.LNil, lua.LString("cancelled")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{anyToLua(l, resp.Value), lua.LNil}
}

// AsyncCancelYield wraps AsyncCancelCmd for Lua.
type AsyncCancelYield struct {
	*funcapi.AsyncCancelCmd
}

var asyncCancelYieldPool = sync.Pool{New: func() any { return &AsyncCancelYield{} }}

func AcquireAsyncCancelYield() *AsyncCancelYield {
	y := asyncCancelYieldPool.Get().(*AsyncCancelYield)
	y.AsyncCancelCmd = funcapi.AcquireAsyncCancelCmd()
	return y
}

func ReleaseAsyncCancelYield(y *AsyncCancelYield) {
	if y.AsyncCancelCmd != nil {
		y.AsyncCancelCmd.Release()
		y.AsyncCancelCmd = nil
	}
	asyncCancelYieldPool.Put(y)
}

func (y *AsyncCancelYield) String() string                { return "<func_async_cancel_yield>" }
func (y *AsyncCancelYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *AsyncCancelYield) ToCommand() dispatcher.Command { return y.AsyncCancelCmd }
func (y *AsyncCancelYield) CmdID() dispatcher.CommandID   { return funcapi.CmdAsyncCancel }
func (y *AsyncCancelYield) Release()                      { ReleaseAsyncCancelYield(y) }
