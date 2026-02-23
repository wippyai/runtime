// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/dispatcher"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/future"
)

// OpenYield wraps OpenCmd for Lua.
type OpenYield struct {
	*contract.OpenCmd
}

var openYieldPool = sync.Pool{New: func() any { return &OpenYield{} }}

func AcquireOpenYield() *OpenYield {
	y := openYieldPool.Get().(*OpenYield)
	y.OpenCmd = contract.AcquireOpenCmd()
	return y
}

func ReleaseOpenYield(y *OpenYield) {
	if y.OpenCmd != nil {
		y.OpenCmd.Release()
		y.OpenCmd = nil
	}
	openYieldPool.Put(y)
}

func (y *OpenYield) String() string                { return "<contract_open_yield>" }
func (y *OpenYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *OpenYield) ToCommand() dispatcher.Command { return y.OpenCmd }
func (y *OpenYield) CmdID() dispatcher.CommandID   { return contract.Open }
func (y *OpenYield) Release()                      { ReleaseOpenYield(y) }

// HandleResult converts open response to Lua values.
func (y *OpenYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "open failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if data == nil {
		luaErr := lua.NewLuaError(l, "no response received").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	resp, ok := data.(contract.OpenResult)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if resp.Error != nil {
		luaErr := lua.WrapErrorWithLua(l, resp.Error, "")
		return []lua.LValue{lua.LNil, luaErr}
	}

	// Wrap instance in userdata
	wrapper := &InstanceWrapper{
		instance: resp.Instance,
	}
	return []lua.LValue{value.NewTypedUserData(l, wrapper, instanceTypeName), lua.LNil}
}

// CallYield wraps CallCmd for Lua.
type CallYield struct {
	*contract.CallCmd
}

var callYieldPool = sync.Pool{New: func() any { return &CallYield{} }}

func AcquireCallYield() *CallYield {
	y := callYieldPool.Get().(*CallYield)
	y.CallCmd = contract.AcquireCallCmd()
	return y
}

func ReleaseCallYield(y *CallYield) {
	if y.CallCmd != nil {
		y.CallCmd.Release()
		y.CallCmd = nil
	}
	callYieldPool.Put(y)
}

func (y *CallYield) String() string                { return "<contract_call_yield>" }
func (y *CallYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *CallYield) ToCommand() dispatcher.Command { return y.CallCmd }
func (y *CallYield) CmdID() dispatcher.CommandID   { return contract.Call }
func (y *CallYield) Release()                      { ReleaseCallYield(y) }

// HandleResult converts call response to Lua values.
func (y *CallYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "call failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if data == nil {
		luaErr := lua.NewLuaError(l, "no response received").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	resp, ok := data.(contract.CallResult)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if resp.Error != nil {
		luaErr := lua.WrapErrorWithLua(l, resp.Error, "")
		return []lua.LValue{lua.LNil, luaErr}
	}
	lv, convErr := luaconv.GoToLua(resp.Value)
	if convErr != nil {
		luaErr := lua.WrapErrorWithLua(l, convErr, "result conversion failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lv, lua.LNil}
}

// AsyncCallYield wraps AsyncCallCmd for Lua.
type AsyncCallYield struct {
	*contract.AsyncCallCmd
	Future *future.Future
}

var asyncCallYieldPool = sync.Pool{New: func() any { return &AsyncCallYield{} }}

func AcquireAsyncCallYield() *AsyncCallYield {
	y := asyncCallYieldPool.Get().(*AsyncCallYield)
	y.AsyncCallCmd = contract.AcquireAsyncCallCmd()
	return y
}

func ReleaseAsyncCallYield(y *AsyncCallYield) {
	if y.AsyncCallCmd != nil {
		y.AsyncCallCmd.Release()
		y.AsyncCallCmd = nil
	}
	y.Future = nil
	asyncCallYieldPool.Put(y)
}

func (y *AsyncCallYield) String() string                { return "<contract_async_call_yield>" }
func (y *AsyncCallYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *AsyncCallYield) ToCommand() dispatcher.Command { return y.AsyncCallCmd }
func (y *AsyncCallYield) CmdID() dispatcher.CommandID   { return contract.AsyncCall }
func (y *AsyncCallYield) Release()                      { ReleaseAsyncCallYield(y) }

// HandleResult returns the pre-created Future after dispatcher confirms start.
func (y *AsyncCallYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "async call failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	resp, ok := data.(contract.AsyncCallResult)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if resp.Error != nil {
		luaErr := lua.WrapErrorWithLua(l, resp.Error, "async call error").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{value.NewTypedUserData(l, y.Future, future.TypeName), lua.LNil}
}

// AsyncCancelYield wraps AsyncCancelCmd for Lua.
type AsyncCancelYield struct {
	*contract.AsyncCancelCmd
}

var asyncCancelYieldPool = sync.Pool{New: func() any { return &AsyncCancelYield{} }}

func AcquireAsyncCancelYield() *AsyncCancelYield {
	y := asyncCancelYieldPool.Get().(*AsyncCancelYield)
	y.AsyncCancelCmd = contract.AcquireAsyncCancelCmd()
	return y
}

func ReleaseAsyncCancelYield(y *AsyncCancelYield) {
	if y.AsyncCancelCmd != nil {
		y.AsyncCancelCmd.Release()
		y.AsyncCancelCmd = nil
	}
	asyncCancelYieldPool.Put(y)
}

func (y *AsyncCancelYield) String() string                { return "<contract_async_cancel_yield>" }
func (y *AsyncCancelYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *AsyncCancelYield) ToCommand() dispatcher.Command { return y.AsyncCancelCmd }
func (y *AsyncCancelYield) CmdID() dispatcher.CommandID   { return contract.AsyncCancel }
func (y *AsyncCancelYield) Release()                      { ReleaseAsyncCancelYield(y) }

// HandleResult for cancel - returns true on success.
func (y *AsyncCancelYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "async cancel failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}
