// Package tty provides terminal input events, styles, and rendering for Lua scripts.
package tty

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/service/terminal"
	"github.com/wippyai/runtime/runtime/lua/engine"
	svcterm "github.com/wippyai/runtime/service/terminal"
)

// Module is the tty module definition.
var Module = &luaapi.ModuleDef{
	Name:        "tty",
	Description: "Terminal input events, styles, and rendering",
	Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	// Force TrueColor so lipgloss renders properly in terminal processes
	lipgloss.SetColorProfile(termenv.TrueColor)

	mod := lua.CreateTable(0, 12)

	// Input event functions
	mod.RawSetString("start", lua.LGoFunc(ttyStart))
	mod.RawSetString("stop", lua.LGoFunc(ttyStop))
	mod.RawSetString("screen_size", lua.LGoFunc(ttyScreenSize))
	mod.RawSetString("events", lua.LGoFunc(ttyEvents))
	mod.RawSetString("mouse", lua.LGoFunc(ttyMouse))

	// Style
	mod.RawSetString("style", lua.LGoFunc(ttyStyleNew))

	// Border constants
	borders := lua.CreateTable(0, 5)
	borders.RawSetString("NORMAL", lua.LString("normal"))
	borders.RawSetString("ROUNDED", lua.LString("rounded"))
	borders.RawSetString("THICK", lua.LString("thick"))
	borders.RawSetString("DOUBLE", lua.LString("double"))
	borders.RawSetString("HIDDEN", lua.LString("hidden"))
	borders.Immutable = true
	mod.RawSetString("borders", borders)

	// Alignment constants
	align := lua.CreateTable(0, 3)
	align.RawSetString("LEFT", lua.LNumber(0))
	align.RawSetString("CENTER", lua.LNumber(0.5))
	align.RawSetString("RIGHT", lua.LNumber(1))
	align.Immutable = true
	mod.RawSetString("align", align)

	// Text utilities
	text := lua.CreateTable(0, 11)
	text.RawSetString("width", lua.LGoFunc(textWidth))
	text.RawSetString("height", lua.LGoFunc(textHeight))
	text.RawSetString("size", lua.LGoFunc(textSize))
	text.RawSetString("join_horizontal", lua.LGoFunc(textJoinHorizontal))
	text.RawSetString("join_vertical", lua.LGoFunc(textJoinVertical))
	text.RawSetString("max_width", lua.LGoFunc(textMaxWidth))
	text.RawSetString("max_height", lua.LGoFunc(textMaxHeight))
	text.RawSetString("place", lua.LGoFunc(textPlace))
	text.RawSetString("place_horizontal", lua.LGoFunc(textPlaceHorizontal))
	text.RawSetString("place_vertical", lua.LGoFunc(textPlaceVertical))

	pos := lua.CreateTable(0, 5)
	pos.RawSetString("TOP", lua.LNumber(0))
	pos.RawSetString("LEFT", lua.LNumber(0))
	pos.RawSetString("CENTER", lua.LNumber(0.5))
	pos.RawSetString("BOTTOM", lua.LNumber(1))
	pos.RawSetString("RIGHT", lua.LNumber(1))
	pos.Immutable = true
	text.RawSetString("position", pos)
	text.Immutable = true
	mod.RawSetString("text", text)

	// Key binding
	mod.RawSetString("bind", lua.LGoFunc(ttyBind))

	mod.Immutable = true

	yields := make([]luaapi.YieldType, len(yieldTypes))
	for i, yt := range yieldTypes {
		yields[i] = luaapi.YieldType{Sample: yt.Sample, CmdID: yt.CmdID}
	}

	return mod, yields
}

// ttyStart starts the terminal input reader (yielding).
func ttyStart(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no terminal context").
			WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}
	if tc.Input == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "input controller unavailable").
			WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}

	yield := AcquireStartInputYield()
	l.Push(yield)
	return -1
}

// ttyStop stops the terminal input reader (yielding).
func ttyStop(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no terminal context").
			WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}
	if tc.Input == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "input controller unavailable").
			WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}

	yield := AcquireStopInputYield()
	l.Push(yield)
	return -1
}

// ttyScreenSize queries the terminal screen size (yielding).
func ttyScreenSize(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no terminal context").
			WithKind(lua.Unavailable).WithRetryable(false))
		return 3
	}
	if tc.Input == nil {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "input controller unavailable").
			WithKind(lua.Unavailable).WithRetryable(false))
		return 3
	}

	yield := AcquireScreenSizeYield()
	l.Push(yield)
	return -1
}

// ttyMouse enables or disables mouse event tracking (yielding).
func ttyMouse(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no terminal context").
			WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}
	if tc.Input == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "input controller unavailable").
			WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}

	enable := l.CheckBool(1)
	if enable {
		yield := AcquireEnableMouseYield()
		l.Push(yield)
	} else {
		yield := AcquireDisableMouseYield()
		l.Push(yield)
	}
	return -1
}

// ttyEvents subscribes to the @tty/events topic and returns a channel.
func ttyEvents(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").
			WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	_, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no process PID").
			WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	proc := engine.GetProcess(l)
	if proc == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no process context").
			WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	ch := engine.NewChannel(64)

	if err := proc.SubscribeExisting(svcterm.TopicTTYEvents, ch); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "subscribe tty events"))
		return 2
	}

	proc.SetTopicHandler(svcterm.TopicTTYEvents, eventHandler)
	engine.PushChannel(l, ch)
	return 1
}
