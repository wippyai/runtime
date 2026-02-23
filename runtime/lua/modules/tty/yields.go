// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

var yieldTypes = []luaYieldType{
	{Sample: &StartInputYield{}, CmdID: ttyapi.StartInput},
	{Sample: &StopInputYield{}, CmdID: ttyapi.StopInput},
	{Sample: &ScreenSizeYield{}, CmdID: ttyapi.ScreenSize},
	{Sample: &EnableMouseYield{}, CmdID: ttyapi.EnableMouse},
	{Sample: &DisableMouseYield{}, CmdID: ttyapi.DisableMouse},
}

type luaYieldType = struct {
	Sample any
	CmdID  dispatcher.CommandID
}

// StartInputYield yields a start-input request.
type StartInputYield struct{}

var startInputYieldPool = sync.Pool{New: func() any { return &StartInputYield{} }}

func AcquireStartInputYield() *StartInputYield {
	return startInputYieldPool.Get().(*StartInputYield)
}

func ReleaseStartInputYield(y *StartInputYield) {
	startInputYieldPool.Put(y)
}

func (y *StartInputYield) String() string                { return "<tty_start_input_yield>" }
func (y *StartInputYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *StartInputYield) CmdID() dispatcher.CommandID   { return ttyapi.StartInput }
func (y *StartInputYield) ToCommand() dispatcher.Command { return ttyapi.StartInputCmd{} }
func (y *StartInputYield) Release()                      { ReleaseStartInputYield(y) }

func (y *StartInputYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	return handleBoolResult(l, data, err, "start input")
}

// StopInputYield yields a stop-input request.
type StopInputYield struct{}

var stopInputYieldPool = sync.Pool{New: func() any { return &StopInputYield{} }}

func AcquireStopInputYield() *StopInputYield {
	return stopInputYieldPool.Get().(*StopInputYield)
}

func ReleaseStopInputYield(y *StopInputYield) {
	stopInputYieldPool.Put(y)
}

func (y *StopInputYield) String() string                { return "<tty_stop_input_yield>" }
func (y *StopInputYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *StopInputYield) CmdID() dispatcher.CommandID   { return ttyapi.StopInput }
func (y *StopInputYield) ToCommand() dispatcher.Command { return ttyapi.StopInputCmd{} }
func (y *StopInputYield) Release()                      { ReleaseStopInputYield(y) }

func (y *StopInputYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	return handleBoolResult(l, data, err, "stop input")
}

// ScreenSizeYield yields a screen-size query.
type ScreenSizeYield struct{}

var screenSizeYieldPool = sync.Pool{New: func() any { return &ScreenSizeYield{} }}

func AcquireScreenSizeYield() *ScreenSizeYield {
	return screenSizeYieldPool.Get().(*ScreenSizeYield)
}

func ReleaseScreenSizeYield(y *ScreenSizeYield) {
	screenSizeYieldPool.Put(y)
}

func (y *ScreenSizeYield) String() string                { return "<tty_screen_size_yield>" }
func (y *ScreenSizeYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *ScreenSizeYield) CmdID() dispatcher.CommandID   { return ttyapi.ScreenSize }
func (y *ScreenSizeYield) ToCommand() dispatcher.Command { return ttyapi.ScreenSizeCmd{} }
func (y *ScreenSizeYield) Release()                      { ReleaseScreenSizeYield(y) }

func (y *ScreenSizeYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LNil, lua.WrapErrorWithLua(l, err, "screen size")}
	}
	switch v := data.(type) {
	case []int:
		if len(v) == 2 {
			return []lua.LValue{lua.LNumber(v[0]), lua.LNumber(v[1]), lua.LNil}
		}
		return []lua.LValue{lua.LNil, lua.LNil, lua.NewLuaError(l, "invalid screen size response").
			WithKind(lua.Internal).WithRetryable(false)}
	default:
		return []lua.LValue{lua.LNil, lua.LNil, lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).WithRetryable(false)}
	}
}

// EnableMouseYield yields an enable-mouse request.
type EnableMouseYield struct{}

var enableMouseYieldPool = sync.Pool{New: func() any { return &EnableMouseYield{} }}

func AcquireEnableMouseYield() *EnableMouseYield {
	return enableMouseYieldPool.Get().(*EnableMouseYield)
}

func ReleaseEnableMouseYield(y *EnableMouseYield) {
	enableMouseYieldPool.Put(y)
}

func (y *EnableMouseYield) String() string                { return "<tty_enable_mouse_yield>" }
func (y *EnableMouseYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *EnableMouseYield) CmdID() dispatcher.CommandID   { return ttyapi.EnableMouse }
func (y *EnableMouseYield) ToCommand() dispatcher.Command { return ttyapi.EnableMouseCmd{} }
func (y *EnableMouseYield) Release()                      { ReleaseEnableMouseYield(y) }

func (y *EnableMouseYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	return handleBoolResult(l, data, err, "enable mouse")
}

// DisableMouseYield yields a disable-mouse request.
type DisableMouseYield struct{}

var disableMouseYieldPool = sync.Pool{New: func() any { return &DisableMouseYield{} }}

func AcquireDisableMouseYield() *DisableMouseYield {
	return disableMouseYieldPool.Get().(*DisableMouseYield)
}

func ReleaseDisableMouseYield(y *DisableMouseYield) {
	disableMouseYieldPool.Put(y)
}

func (y *DisableMouseYield) String() string                { return "<tty_disable_mouse_yield>" }
func (y *DisableMouseYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *DisableMouseYield) CmdID() dispatcher.CommandID   { return ttyapi.DisableMouse }
func (y *DisableMouseYield) ToCommand() dispatcher.Command { return ttyapi.DisableMouseCmd{} }
func (y *DisableMouseYield) Release()                      { ReleaseDisableMouseYield(y) }

func (y *DisableMouseYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	return handleBoolResult(l, data, err, "disable mouse")
}

func handleBoolResult(l *lua.LState, data any, err error, op string) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, op)}
	}
	switch v := data.(type) {
	case bool:
		if v {
			return []lua.LValue{lua.LTrue, lua.LNil}
		}
		return []lua.LValue{lua.LFalse, lua.LNil}
	case nil:
		return []lua.LValue{lua.LTrue, lua.LNil}
	default:
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).WithRetryable(false)}
	}
}
