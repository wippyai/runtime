// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"fmt"

	lua "github.com/wippyai/go-lua"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const (
	surfaceTypeName = "tty.Surface"
	maxSurfaceRows  = 16384
)

func init() {
	value.RegisterTypeMethods(nil, surfaceTypeName,
		map[string]lua.LGoFunc{
			"__tostring": surfaceToString,
			"__gc":       surfaceGC,
		},
		map[string]lua.LGoFunc{
			"present":    surfacePresent,
			"invalidate": surfaceInvalidate,
			"close":      surfaceClose,
		})
}

// surfaceWrapper is only the Lua ownership handle. Diffing and terminal state
// belong to the selected tty.Surface backend.
type surfaceWrapper struct {
	backend ttyapi.Surface
}

func ttySurfaceNew(l *lua.LState) int {
	port, portErr := ttyapi.GetPort(l.Context())
	if portErr != nil || port == nil {
		l.Push(lua.LNil)
		if portErr != nil {
			l.Push(lua.WrapErrorWithLua(l, portErr, "resolve terminal port"))
		} else {
			l.Push(lua.NewLuaError(l, "terminal output unavailable").
				WithKind(lua.Unavailable).WithRetryable(false))
		}
		return 2
	}

	opts := ttyapi.SurfaceOptions{}
	if options := l.OptTable(1, nil); options != nil {
		var err error
		if opts.AlternateScreen, err = optionBool(options, "alternate_screen"); err != nil {
			return invalidArgument(l, err.Error())
		}
		if opts.HideCursor, err = optionBool(options, "hide_cursor"); err != nil {
			return invalidArgument(l, err.Error())
		}
		if opts.Synchronized, err = optionBool(options, "synchronized_output"); err != nil {
			return invalidArgument(l, err.Error())
		}
	}
	backend, err := port.OpenSurface(opts)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "open terminal surface"))
		return 2
	}
	if backend == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "terminal output unavailable").WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}
	surface := &surfaceWrapper{backend: backend}
	value.PushTypedUserData(l, surface, surfaceTypeName)
	l.Push(lua.LNil)
	return 2
}

func checkSurface(l *lua.LState) *surfaceWrapper {
	ud := l.CheckUserData(1)
	if surface, ok := ud.Value.(*surfaceWrapper); ok {
		return surface
	}
	l.ArgError(1, "tty.Surface expected")
	return nil
}

func surfaceToString(l *lua.LState) int {
	l.Push(lua.LString("tty.Surface{}"))
	return 1
}

func surfacePresent(l *lua.LState) int {
	surface := checkSurface(l)
	if surface == nil {
		return 0
	}
	table := l.CheckTable(2)
	rowCount := table.Len()
	if rowCount > maxSurfaceRows {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "surface frame exceeds row limit").
			WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	rows := make([]string, rowCount)
	for index := 1; index <= rowCount; index++ {
		value := table.RawGetInt(index)
		if value.Type() != lua.LTString {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l,
				fmt.Sprintf("surface row %d must be a string", index)).
				WithKind(lua.Invalid).WithRetryable(false))
			return 2
		}
		rows[index-1] = string(value.(lua.LString))
	}
	frame := ttyapi.Frame{Rows: rows}
	if options := l.OptTable(3, nil); options != nil {
		if value := options.RawGetString("cursor"); value != lua.LNil {
			cursor, ok := value.(*lua.LTable)
			if !ok {
				l.Push(lua.LNil)
				l.Push(lua.NewLuaError(l, "surface cursor must be a table").
					WithKind(lua.Invalid).WithRetryable(false))
				return 2
			}
			x, ok := integerValue(cursor.RawGetString("x"))
			if !ok || x < 1 || x > maxTerminalDimension {
				return invalidArgument(l, "surface cursor x must be a positive bounded integer")
			}
			y, ok := integerValue(cursor.RawGetString("y"))
			if !ok || y < 1 || y > maxTerminalDimension {
				return invalidArgument(l, "surface cursor y must be a positive bounded integer")
			}
			visible, err := requiredOptionBool(cursor, "visible", "surface cursor")
			if err != nil {
				return invalidArgument(l, err.Error())
			}
			frame.Cursor = &ttyapi.Cursor{
				Column: x - 1, Row: y - 1,
				Visible: visible,
			}
		}
	}

	changed, written, err := surface.presentFrame(frame)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "present terminal surface"))
		return 2
	}
	stats := lua.CreateTable(0, 3)
	stats.RawSetString("rows", lua.LInteger(rowCount))
	stats.RawSetString("changed_rows", lua.LInteger(changed))
	stats.RawSetString("bytes_written", lua.LInteger(written))
	stats.Immutable = true
	l.Push(stats)
	l.Push(lua.LNil)
	return 2
}

func optionBool(options *lua.LTable, field string) (bool, error) {
	value := options.RawGetString(field)
	if value == lua.LNil {
		return false, nil
	}
	boolean, ok := value.(lua.LBool)
	if !ok {
		return false, fmt.Errorf("surface option %s must be a boolean", field)
	}
	return bool(boolean), nil
}

func requiredOptionBool(options *lua.LTable, field, owner string) (bool, error) {
	value, ok := options.RawGetString(field).(lua.LBool)
	if !ok {
		return false, fmt.Errorf("%s %s must be a boolean", owner, field)
	}
	return bool(value), nil
}

func (s *surfaceWrapper) present(rows []string) (int, int, error) {
	return s.presentFrame(ttyapi.Frame{Rows: rows})
}

func (s *surfaceWrapper) presentFrame(frame ttyapi.Frame) (int, int, error) {
	var stats ttyapi.PresentStats
	var err error
	stats, err = s.backend.Present(frame)
	return stats.ChangedRows, stats.Bytes, err
}

func surfaceInvalidate(l *lua.LState) int {
	surface := checkSurface(l)
	if surface == nil {
		return 0
	}
	surface.backend.Invalidate()
	l.Push(lua.LTrue)
	return 1
}

func surfaceClose(l *lua.LState) int {
	surface := checkSurface(l)
	if surface == nil {
		return 0
	}
	if err := surface.close(); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "close terminal surface"))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func surfaceGC(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if surface, ok := ud.Value.(*surfaceWrapper); ok {
		_ = surface.close()
	}
	return 0
}

func (s *surfaceWrapper) close() error {
	return s.backend.Close()
}
