package funcs

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	funcapi "github.com/wippyai/runtime/api/dispatcher/func"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	lua "github.com/yuin/gopher-lua"
)

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
	fmt.Printf("[DEBUG] CallYield.HandleResult called: data=%v, err=%v\n", data, err)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "call failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if data == nil {
		luaErr := lua.NewLuaError(l, "no response received").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	resp, ok := data.(funcapi.Response)
	if !ok {
		fmt.Printf("[DEBUG] type assertion failed: got %T\n", data)
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if resp.Error != nil {
		luaErr := lua.WrapErrorWithLua(l, resp.Error, "function error").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	lv, convErr := luaconv.GoToLua(resp.Value)
	if convErr != nil {
		luaErr := lua.WrapErrorWithLua(l, convErr, "result conversion failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lv, lua.LNil}
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

// HandleResult converts async start response to Future userdata.
func (y *AsyncStartYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "async start failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	resp, ok := data.(funcapi.AsyncStartResponse)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if resp.Error != nil {
		luaErr := lua.WrapErrorWithLua(l, resp.Error, "async start error").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	// Return Future userdata instead of raw CallID
	return []lua.LValue{createFuture(l, resp.CallID), lua.LNil}
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
		luaErr := lua.WrapErrorWithLua(l, err, "async await failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	resp, ok := data.(funcapi.AsyncAwaitResponse)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if resp.Cancelled {
		luaErr := lua.NewLuaError(l, "call cancelled").
			WithKind(lua.KindCanceled).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if resp.Error != nil {
		luaErr := lua.WrapErrorWithLua(l, resp.Error, "async await error").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	lv, convErr := luaconv.GoToLua(resp.Value)
	if convErr != nil {
		luaErr := lua.WrapErrorWithLua(l, convErr, "result conversion failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lv, lua.LNil}
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

// HandleResult for cancel - returns nil, nil (no meaningful result).
func (y *AsyncCancelYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "async cancel failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}
