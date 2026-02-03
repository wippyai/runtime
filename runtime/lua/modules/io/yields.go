package io

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/service/terminal"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

var yieldTypes = []luaapi.YieldType{
	{Sample: &ReadYield{}, CmdID: ttyapi.Read},
	{Sample: &ReadLineYield{}, CmdID: ttyapi.ReadLine},
	{Sample: &RawEnableYield{}, CmdID: ttyapi.RawEnable},
	{Sample: &RawDisableYield{}, CmdID: ttyapi.RawDisable},
}

// ReadYield yields a read request to the dispatcher.
type ReadYield struct {
	Size int
}

var readYieldPool = sync.Pool{New: func() any { return &ReadYield{} }}

func AcquireReadYield(size int) *ReadYield {
	if size <= 0 {
		size = ttyapi.DefaultReadSize
	}
	y := readYieldPool.Get().(*ReadYield)
	y.Size = size
	return y
}

func ReleaseReadYield(y *ReadYield) {
	y.Size = 0
	readYieldPool.Put(y)
}

func (y *ReadYield) String() string                { return "<io_read_yield>" }
func (y *ReadYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *ReadYield) CmdID() dispatcher.CommandID   { return ttyapi.Read }
func (y *ReadYield) ToCommand() dispatcher.Command { return ttyapi.ReadCmd{Size: y.Size} }
func (y *ReadYield) Release()                      { ReleaseReadYield(y) }

func (y *ReadYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	switch v := data.(type) {
	case []byte:
		return []lua.LValue{lua.LString(v), lua.LNil}
	case string:
		return []lua.LValue{lua.LString(v), lua.LNil}
	default:
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
}

// ReadLineYield yields a line read request to the dispatcher.
type ReadLineYield struct{}

var readLineYieldPool = sync.Pool{New: func() any { return &ReadLineYield{} }}

func AcquireReadLineYield() *ReadLineYield {
	return readLineYieldPool.Get().(*ReadLineYield)
}

func ReleaseReadLineYield(y *ReadLineYield) {
	readLineYieldPool.Put(y)
}

func (y *ReadLineYield) String() string                { return "<io_readline_yield>" }
func (y *ReadLineYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *ReadLineYield) CmdID() dispatcher.CommandID   { return ttyapi.ReadLine }
func (y *ReadLineYield) ToCommand() dispatcher.Command { return ttyapi.ReadLineCmd{} }
func (y *ReadLineYield) Release()                      { ReleaseReadLineYield(y) }

func (y *ReadLineYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	switch v := data.(type) {
	case string:
		return []lua.LValue{lua.LString(v), lua.LNil}
	case []byte:
		return []lua.LValue{lua.LString(v), lua.LNil}
	default:
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
}

// RawEnableYield yields a raw-enable request to the dispatcher.
type RawEnableYield struct{}

var rawEnableYieldPool = sync.Pool{New: func() any { return &RawEnableYield{} }}

func AcquireRawEnableYield() *RawEnableYield {
	return rawEnableYieldPool.Get().(*RawEnableYield)
}

func ReleaseRawEnableYield(y *RawEnableYield) {
	rawEnableYieldPool.Put(y)
}

func (y *RawEnableYield) String() string                { return "<io_raw_enable_yield>" }
func (y *RawEnableYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *RawEnableYield) CmdID() dispatcher.CommandID   { return ttyapi.RawEnable }
func (y *RawEnableYield) ToCommand() dispatcher.Command { return ttyapi.RawEnableCmd{} }
func (y *RawEnableYield) Release()                      { ReleaseRawEnableYield(y) }

func (y *RawEnableYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	return handleRawResult(l, data, err)
}

// RawDisableYield yields a raw-disable request to the dispatcher.
type RawDisableYield struct{}

var rawDisableYieldPool = sync.Pool{New: func() any { return &RawDisableYield{} }}

func AcquireRawDisableYield() *RawDisableYield {
	return rawDisableYieldPool.Get().(*RawDisableYield)
}

func ReleaseRawDisableYield(y *RawDisableYield) {
	rawDisableYieldPool.Put(y)
}

func (y *RawDisableYield) String() string                { return "<io_raw_disable_yield>" }
func (y *RawDisableYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *RawDisableYield) CmdID() dispatcher.CommandID   { return ttyapi.RawDisable }
func (y *RawDisableYield) ToCommand() dispatcher.Command { return ttyapi.RawDisableCmd{} }
func (y *RawDisableYield) Release()                      { ReleaseRawDisableYield(y) }

func (y *RawDisableYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	return handleRawResult(l, data, err)
}

func handleRawResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
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
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
}

// ioReadYielding reads n bytes from stdin (yielding).
// io.read(n) -> string, err
func ioReadYielding(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdin == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no terminal context"))
		return 2
	}

	n := l.OptInt(1, ttyapi.DefaultReadSize)
	if n <= 0 {
		n = ttyapi.DefaultReadSize
	}

	yield := AcquireReadYield(n)
	l.Push(yield)
	return -1
}

// ioReadlineYielding reads a line from stdin (yielding).
// io.readline() -> string, err
func ioReadlineYielding(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdin == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no terminal context"))
		return 2
	}

	yield := AcquireReadLineYield()
	l.Push(yield)
	return -1
}

// ioRaw enables or disables raw terminal mode (yielding).
// io.raw(enable?) -> boolean, err
func ioRaw(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdin == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no terminal context"))
		return 2
	}
	if tc.Raw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("raw terminal control unavailable"))
		return 2
	}

	enable := l.OptBool(1, true)
	if enable {
		yield := AcquireRawEnableYield()
		l.Push(yield)
		return -1
	}

	yield := AcquireRawDisableYield()
	l.Push(yield)
	return -1
}
