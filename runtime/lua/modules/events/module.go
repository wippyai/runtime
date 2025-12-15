// Package events provides event bus subscribe and send operations for Lua.
package events

import (
	"fmt"
	"sync/atomic"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

var subscriptionCounter uint64

// Module is the events module definition.
var Module = &luaapi.ModuleDef{
	Name:        "events",
	Description: "Event bus subscribe and send",
	Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 2)
	mod.RawSetString("subscribe", lua.LGoFunc(subscribe))
	mod.RawSetString("send", lua.LGoFunc(send))
	mod.Immutable = true

	yields := []luaapi.YieldType{
		{Sample: &EventSubscribeYield{}, CmdID: event.Subscribe},
		{Sample: &EventSendYield{}, CmdID: event.Send},
	}

	return mod, yields
}

func subscribe(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	system := l.CheckString(1)
	if system == "" {
		l.Push(lua.LNil)
		err := lua.NewLuaError(l, "system pattern is required").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(err)
		return 2
	}

	if !security.IsAllowed(ctx, "events.subscribe", system, nil) {
		l.Push(lua.LNil)
		err := lua.NewLuaError(l, fmt.Sprintf("not allowed to subscribe to events from system: %s", system)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(err)
		return 2
	}

	var kind string
	if l.GetTop() >= 2 && l.Get(2) != lua.LNil {
		kind = l.CheckString(2)
	}

	pid, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.Push(lua.LNil)
		err := lua.NewLuaError(l, "no process PID").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(err)
		return 2
	}

	// Create channel and unique topic
	ch := engine.NewChannel(64)
	subID := atomic.AddUint64(&subscriptionCounter, 1)
	topic := fmt.Sprintf("events@%d", subID)

	yield := AcquireEventSubscribeYield(system, kind, ch, pid, topic)
	l.Push(yield)
	return -1
}

func send(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	system := l.CheckString(1)
	if system == "" {
		l.Push(lua.LNil)
		err := lua.NewLuaError(l, "system is required").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(err)
		return 2
	}

	kind := l.CheckString(2)
	if kind == "" {
		l.Push(lua.LNil)
		err := lua.NewLuaError(l, "kind is required").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(err)
		return 2
	}

	path := l.CheckString(3)
	if path == "" {
		l.Push(lua.LNil)
		err := lua.NewLuaError(l, "path is required").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(err)
		return 2
	}

	if !security.IsAllowed(ctx, "events.send", system, nil) {
		l.Push(lua.LNil)
		err := lua.NewLuaError(l, fmt.Sprintf("not allowed to send events to system: %s", system)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(err)
		return 2
	}

	var data any
	if l.GetTop() >= 4 && l.Get(4) != lua.LNil {
		data = value.ToGoAny(l.Get(4))
	}

	yield := AcquireEventSendYield(system, kind, path, data)
	l.Push(yield)
	return -1
}
