package funcs

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/function"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/future"
	lua "github.com/yuin/gopher-lua"
)

// CallYield wraps CallCmd for Lua.
type CallYield struct {
	*function.CallCmd
}

var callYieldPool = sync.Pool{New: func() any { return &CallYield{} }}

func AcquireCallYield() *CallYield {
	y := callYieldPool.Get().(*CallYield)
	y.CallCmd = function.AcquireCallCmd()
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
func (y *CallYield) CmdID() dispatcher.CommandID   { return function.Call }
func (y *CallYield) Release()                      { ReleaseCallYield(y) }

// HandleResult converts function call response to Lua values.
func (y *CallYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "call failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if data == nil {
		luaErr := lua.NewLuaError(l, "no response received").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	resp, ok := data.(function.CallResult)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if resp.Error != nil {
		// Wrap error but preserve original kind/retryable from error chain
		luaErr := lua.WrapErrorWithLua(l, resp.Error, "")
		return []lua.LValue{lua.LNil, luaErr}
	}
	lv, convErr := luaconv.GoToLua(resp.Value)
	if convErr != nil {
		luaErr := lua.WrapErrorWithLua(l, convErr, "result conversion failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lv, lua.LNil}
}

// AsyncStartYield wraps AsyncStartCmd for Lua.
type AsyncStartYield struct {
	*function.AsyncStartCmd
	Future *future.Future // Pre-created Future to return
}

var asyncStartYieldPool = sync.Pool{New: func() any { return &AsyncStartYield{} }}

func AcquireAsyncStartYield() *AsyncStartYield {
	y := asyncStartYieldPool.Get().(*AsyncStartYield)
	y.AsyncStartCmd = function.AcquireAsyncStartCmd()
	return y
}

func ReleaseAsyncStartYield(y *AsyncStartYield) {
	if y.AsyncStartCmd != nil {
		y.AsyncStartCmd.Release()
		y.AsyncStartCmd = nil
	}
	y.Future = nil
	asyncStartYieldPool.Put(y)
}

func (y *AsyncStartYield) String() string                { return "<func_async_start_yield>" }
func (y *AsyncStartYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *AsyncStartYield) ToCommand() dispatcher.Command { return y.AsyncStartCmd }
func (y *AsyncStartYield) CmdID() dispatcher.CommandID   { return function.AsyncStart }
func (y *AsyncStartYield) Release()                      { ReleaseAsyncStartYield(y) }

// HandleResult returns the pre-created Future after dispatcher confirms start.
func (y *AsyncStartYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "async start failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	resp, ok := data.(function.AsyncStartResult)
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
		return []lua.LValue{lua.LNil, luaErr}
	}
	// Return pre-created Future with channel
	return []lua.LValue{value.NewTypedUserData(l, y.Future, future.TypeName), lua.LNil}
}

// AsyncCancelYield wraps AsyncCancelCmd for Lua.
type AsyncCancelYield struct {
	*function.AsyncCancelCmd
}

var asyncCancelYieldPool = sync.Pool{New: func() any { return &AsyncCancelYield{} }}

func AcquireAsyncCancelYield() *AsyncCancelYield {
	y := asyncCancelYieldPool.Get().(*AsyncCancelYield)
	y.AsyncCancelCmd = function.AcquireAsyncCancelCmd()
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
func (y *AsyncCancelYield) CmdID() dispatcher.CommandID   { return function.AsyncCancel }
func (y *AsyncCancelYield) Release()                      { ReleaseAsyncCancelYield(y) }

// HandleResult for cancel - returns nil, nil (no meaningful result).
func (y *AsyncCancelYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "async cancel failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}
