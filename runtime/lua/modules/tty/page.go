// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"fmt"

	lua "github.com/wippyai/go-lua"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func pageFromLua(value lua.LValue) (*ttyapi.Page, error) {
	if value == lua.LNil {
		return nil, nil
	}
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("page must be a table with foreground and background #RRGGBB colors")
	}
	fg, fgOK := table.RawGetString("foreground").(lua.LString)
	bg, bgOK := table.RawGetString("background").(lua.LString)
	if !fgOK || !bgOK {
		return nil, fmt.Errorf("page requires both foreground and background #RRGGBB colors")
	}
	page, err := (ttyapi.Page{Foreground: string(fg), Background: string(bg)}).Canonical()
	if err != nil {
		return nil, err
	}
	return &page, nil
}

func viewportSetPage(l *lua.LState) int {
	v := checkViewport(l)
	page, err := pageFromLua(l.Get(2))
	if err != nil {
		return invalidArgument(l, err.Error())
	}
	setter, ok := v.view.(ttyapi.PageViewport)
	if !ok {
		return invalidArgument(l, "viewport does not grant page configuration")
	}
	if err := setter.SetPage(l.Context(), page); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "set viewport page"))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
