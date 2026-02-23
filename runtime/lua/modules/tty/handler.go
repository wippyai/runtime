// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/service/terminal"
)

// eventHandler converts TTYEvent Go structs to Lua tables for channel delivery.
func eventHandler(_ context.Context, l *lua.LState, _ pid.PID, _ string, payloads []payload.Payload) lua.LValue {
	if len(payloads) == 0 {
		return lua.LNil
	}

	p := payloads[0]
	ev, ok := p.Data().(*terminal.TTYEvent)
	if !ok {
		return lua.LNil
	}

	tbl := l.CreateTable(0, 12)
	tbl.RawSetString("type", lua.LString(ev.Type))

	switch ev.Type {
	case "key":
		tbl.RawSetString("key", lua.LString(ev.Key))
		tbl.RawSetString("key_type", lua.LString(ev.KeyType))
		tbl.RawSetString("action", lua.LString(ev.Action))
		tbl.RawSetString("alt", lua.LBool(ev.Alt))
		tbl.RawSetString("ctrl", lua.LBool(ev.Ctrl))
		tbl.RawSetString("shift", lua.LBool(ev.Shift))

	case "mouse":
		tbl.RawSetString("action", lua.LString(ev.Action))
		tbl.RawSetString("button", lua.LString(ev.Button))
		tbl.RawSetString("x", lua.LNumber(ev.X))
		tbl.RawSetString("y", lua.LNumber(ev.Y))
		tbl.RawSetString("alt", lua.LBool(ev.Alt))
		tbl.RawSetString("ctrl", lua.LBool(ev.Ctrl))
		tbl.RawSetString("shift", lua.LBool(ev.Shift))

	case "resize", "start":
		tbl.RawSetString("width", lua.LNumber(ev.Width))
		tbl.RawSetString("height", lua.LNumber(ev.Height))

	case "focus":
		tbl.RawSetString("focused", lua.LBool(ev.Focused))

	case "paste":
		tbl.RawSetString("text", lua.LString(ev.Paste))
	}

	return tbl
}
