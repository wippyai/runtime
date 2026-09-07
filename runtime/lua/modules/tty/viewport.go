// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"fmt"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/pid"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const viewportTypeName = "tty.Viewport"

type viewportWrapper struct {
	view     ttyapi.Viewport
	updates  *updateBridge
	closeErr error
	// Lua-thread-only cache of immutable string values, never returned as a table.
	rows []lua.LValue
	once sync.Once
}

func init() {
	value.RegisterTypeMethods(nil, viewportTypeName,
		map[string]lua.LGoFunc{"__gc": viewportGC, "__tostring": viewportToString},
		map[string]lua.LGoFunc{
			"grant": viewportGrant, "handle": viewportHandle,
			"snapshot": viewportSnapshot, "updates": viewportUpdates, "send": viewportSend,
			"resize": viewportResize, "close": viewportClose,
			"mount": viewportMount, "revoke": viewportRevoke,
			"set_page": viewportSetPage,
		})
}

func ttyAttach(l *lua.LState) int {
	service := ttyapi.GetService(l.Context())
	if service == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "tty service unavailable").WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}
	handle := l.CheckString(1)
	if remote, ok := service.(ttyapi.RemoteService); ok && remote.IsRemote(handle) {
		l.Push(&ViewportIOYield{Command: ttyapi.ViewportIOCmd{Operation: "attach", Handle: handle}})
		return -1
	}
	view, err := service.Attach(l.Context(), handle)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "attach viewport"))
		return 2
	}
	if view == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "attached viewport is unavailable").
			WithKind(lua.Internal).WithRetryable(false))
		return 2
	}
	pushViewport(l, view)
	return 2
}

func ttyViewportNew(l *lua.LState) int {
	service := ttyapi.GetService(l.Context())
	if service == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "tty service unavailable").WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}
	width, height := 80, 24
	var page *ttyapi.Page
	if options := l.OptTable(1, nil); options != nil {
		var err error
		if page, err = pageFromLua(options.RawGetString("page")); err != nil {
			return invalidArgument(l, err.Error())
		}
		if width, err = viewportDimension(options, "width", width); err != nil {
			return invalidArgument(l, err.Error())
		}
		if height, err = viewportDimension(options, "height", height); err != nil {
			return invalidArgument(l, err.Error())
		}
	}
	if err := ttyapi.ValidateViewportSize(width, height); err != nil {
		return invalidArgument(l, err.Error())
	}
	view, err := service.Create(l.Context(), width, height)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "create viewport"))
		return 2
	}
	if view == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "created viewport is unavailable").
			WithKind(lua.Internal).WithRetryable(false))
		return 2
	}
	if page != nil {
		setter, ok := view.(ttyapi.PageViewport)
		if !ok {
			_ = view.Close()
			return invalidArgument(l, "viewport does not support page configuration")
		}
		if err := setter.SetPage(l.Context(), page); err != nil {
			_ = view.Close()
			l.Push(lua.LNil)
			l.Push(lua.WrapErrorWithLua(l, err, "set viewport page"))
			return 2
		}
	}
	pushViewport(l, view)
	return 2
}

func pushViewport(l *lua.LState, view ttyapi.Viewport) {
	value.PushTypedUserData(l, &viewportWrapper{view: view}, viewportTypeName)
	l.Push(lua.LNil)
}

func viewportDimension(options *lua.LTable, field string, defaultValue int) (int, error) {
	value := options.RawGetString(field)
	if value == lua.LNil {
		return defaultValue, nil
	}
	return viewportDimensionValue(value, field)
}

func viewportDimensionValue(value lua.LValue, field string) (int, error) {
	dimension, ok := integerValue(value)
	if !ok || dimension < 1 || dimension > maxTerminalDimension {
		return 0, fmt.Errorf("viewport %s must be an integer between 1 and %d", field, maxTerminalDimension)
	}
	return dimension, nil
}

func checkViewport(l *lua.LState) *viewportWrapper {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*viewportWrapper); ok {
		return v
	}
	l.ArgError(1, "tty.Viewport expected")
	return nil
}

func viewportToString(l *lua.LState) int { l.Push(lua.LString("tty.Viewport{}")); return 1 }

func viewportGrant(l *lua.LState) int {
	view := checkViewport(l).view
	if !checkViewportRight(l, view, "") {
		return 2
	}
	grant := view.Grant()
	if grant == "" {
		return invalidArgument(l, "viewport has no producer grant")
	}
	l.Push(lua.LString(grant))
	l.Push(lua.LNil)
	return 2
}

func viewportHandle(l *lua.LState) int {
	view := checkViewport(l).view
	if !checkViewportRight(l, view, "") {
		return 2
	}
	l.Push(lua.LString(view.Handle()))
	return 1
}

func viewportSnapshot(l *lua.LState) int {
	v := checkViewport(l)
	view := v.view
	if !checkViewportRight(l, view, ttyapi.RightObserve) {
		return 2
	}
	s := view.Snapshot()
	if l.GetTop() >= 2 {
		after, ok := integerValue(l.Get(2))
		if !ok {
			l.ArgError(2, "viewport revision must be an integer")
			return 0
		}
		if after >= 0 && uint64(after) == s.Revision {
			l.Push(lua.LNil)
			return 1
		}
	}
	rows := l.CreateTable(len(s.Rows), 0)
	if len(v.rows) != len(s.Rows) {
		// Replace on resize so a smaller screen does not retain old rows.
		v.rows = make([]lua.LValue, len(s.Rows))
	}
	for i, row := range s.Rows {
		if previous, ok := v.rows[i].(lua.LString); !ok || string(previous) != row {
			v.rows[i] = lua.LString(row)
		}
		rows.RawSetInt(i+1, v.rows[i])
	}
	result := l.CreateTable(0, 4)
	result.RawSetString("revision", lua.LInteger(s.Revision))
	result.RawSetString("width", lua.LInteger(s.Width))
	result.RawSetString("height", lua.LInteger(s.Height))
	result.RawSetString("rows", rows)
	if s.Cursor != nil {
		cursor := l.CreateTable(0, 3)
		cursor.RawSetString("x", lua.LInteger(s.Cursor.Column+1))
		cursor.RawSetString("y", lua.LInteger(s.Cursor.Row+1))
		cursor.RawSetString("visible", lua.LBool(s.Cursor.Visible))
		cursor.Immutable = true
		result.RawSetString("cursor", cursor)
	}
	l.Push(result)
	return 1
}

func viewportUpdates(l *lua.LState) int {
	v := checkViewport(l)
	if !checkViewportRight(l, v.view, ttyapi.RightObserve) {
		return 2
	}
	if v.updates == nil {
		bridge, err := newUpdateBridge(l, v.view)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.WrapErrorWithLua(l, err, "subscribe viewport updates"))
			return 2
		}
		v.updates = bridge
	}
	l.Push(v.updates.value)
	l.Push(lua.LNil)
	return 2
}

func viewportSend(l *lua.LState) int {
	v := checkViewport(l)
	if !checkViewportRight(l, v.view, ttyapi.RightInput) {
		return 2
	}
	event, err := DecodeEvent(l.CheckTable(2))
	if err != nil {
		return invalidArgument(l, err.Error())
	}
	if remote, ok := v.view.(ttyapi.RemoteViewport); ok {
		l.Push(&ViewportIOYield{Command: ttyapi.ViewportIOCmd{Operation: "send", View: remote, Event: event}})
		return -1
	}
	if err := v.view.Send(event); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "send viewport event"))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func viewportResize(l *lua.LState) int {
	v := checkViewport(l)
	if !checkViewportRight(l, v.view, ttyapi.RightResize) {
		return 2
	}
	width, err := viewportDimensionValue(l.Get(2), "width")
	if err != nil {
		return invalidArgument(l, err.Error())
	}
	height, err := viewportDimensionValue(l.Get(3), "height")
	if err != nil {
		return invalidArgument(l, err.Error())
	}
	if err := ttyapi.ValidateViewportSize(width, height); err != nil {
		return invalidArgument(l, err.Error())
	}
	if remote, ok := v.view.(ttyapi.RemoteViewport); ok {
		l.Push(&ViewportIOYield{Command: ttyapi.ViewportIOCmd{Operation: "resize", View: remote, Width: width, Height: height}})
		return -1
	}
	err = v.view.Resize(width, height)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "resize viewport"))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func viewportClose(l *lua.LState) int {
	v := checkViewport(l)
	if !checkViewportRight(l, v.view, "") {
		return 2
	}
	v.close()
	if v.closeErr != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, v.closeErr, "close viewport"))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func (v *viewportWrapper) close() {
	v.once.Do(func() {
		v.rows = nil
		if v.updates != nil {
			v.updates.close()
		}
		v.closeErr = v.view.Close()
	})
}

func viewportGC(l *lua.LState) int { checkViewport(l).close(); return 0 }

func checkViewportRight(l *lua.LState, view ttyapi.Viewport, right string) bool {
	if checked, ok := view.(ttyapi.CheckedViewport); ok {
		if err := checked.Check(l.Context(), right); err != nil {
			l.Push(lua.LNil)
			l.Push(lua.WrapErrorWithLua(l, err, "viewport access"))
			return false
		}
	}
	return true
}
func viewportMount(l *lua.LState) int {
	v := checkViewport(l)
	issuer, ok := v.view.(ttyapi.MountableViewport)
	if !ok {
		return invalidArgument(l, "mounted view cannot delegate")
	}
	target, err := pid.ParsePID(l.CheckString(2))
	if err != nil {
		return invalidArgument(l, "mount recipient must be a process PID")
	}
	opts := l.CheckTable(3)
	var rights ttyapi.MountRights
	if rights.Observe, err = optionBool(opts, "observe"); err != nil {
		return invalidArgument(l, err.Error())
	}
	if rights.Input, err = optionBool(opts, "input"); err != nil {
		return invalidArgument(l, err.Error())
	}
	if rights.Resize, err = optionBool(opts, "resize"); err != nil {
		return invalidArgument(l, err.Error())
	}
	ref, err := issuer.Mount(l.Context(), target, rights)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "mount viewport"))
		return 2
	}
	l.Push(lua.LString(ref))
	l.Push(lua.LNil)
	return 2
}
func viewportRevoke(l *lua.LState) int {
	issuer, ok := checkViewport(l).view.(ttyapi.MountableViewport)
	if !ok {
		return invalidArgument(l, "mounted view cannot revoke")
	}
	if err := issuer.Revoke(l.Context(), l.CheckString(2)); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "revoke viewport mount"))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
