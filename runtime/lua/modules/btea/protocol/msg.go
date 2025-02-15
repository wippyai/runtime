package protocol

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	lua "github.com/yuin/gopher-lua"
	"reflect"
)

var (
	staticState = lua.NewState()
)

func MsgToLua(msg tea.Msg) lua.LValue {
	if msg == nil {
		return lua.LNil
	}

	tbl := staticState.NewTable()
	tbl.RawSetString("type", lua.LString("update"))

	ud := staticState.NewUserData()
	ud.Value = msg
	tbl.RawSetString("opaque", ud)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		keyTbl := staticState.NewTable()
		keyTbl.RawSetString("type", lua.LString("key"))
		keyTbl.RawSetString("alt", lua.LBool(msg.Alt))
		keyTbl.RawSetString("paste", lua.LBool(msg.Paste))

		// Only set runes for actual text input, not for special keys
		if msg.Type == tea.KeyRunes {
			keyTbl.RawSetString("runes", lua.LString(msg.Runes))
			keyTbl.RawSetString("string", lua.LString(msg.String()))
		} else {
			// For special keys, don't set runes
			keyTbl.RawSetString("string", lua.LString(""))
		}

		if keyStr, ok := keyTypeMap[msg.Type]; ok {
			keyTbl.RawSetString("key_type", lua.LString(keyStr))
		} else if msg.Type == tea.KeyRunes {
			keyTbl.RawSetString("key_type", lua.LString("char"))
		}

		tbl.RawSetString("key", keyTbl)

	case tea.MouseMsg:
		mouseTbl := staticState.NewTable()
		mouseTbl.RawSetString("type", lua.LString("mouse"))
		mouseTbl.RawSetString("x", lua.LNumber(msg.X))
		mouseTbl.RawSetString("y", lua.LNumber(msg.Y))
		mouseTbl.RawSetString("alt", lua.LBool(msg.Alt))
		mouseTbl.RawSetString("ctrl", lua.LBool(msg.Ctrl))
		mouseTbl.RawSetString("shift", lua.LBool(msg.Shift))

		switch msg.Action {
		case tea.MouseActionPress:
			mouseTbl.RawSetString("action", lua.LString("press"))
		case tea.MouseActionRelease:
			mouseTbl.RawSetString("action", lua.LString("release"))
		case tea.MouseActionMotion:
			mouseTbl.RawSetString("action", lua.LString("motion"))
		}

		if buttonStr, ok := mouseButtonMap[msg.Button]; ok {
			mouseTbl.RawSetString("button", lua.LString(buttonStr))
		}
		tbl.RawSetString("mouse", mouseTbl)

	case tea.WindowSizeMsg:
		sizeTbl := staticState.NewTable()
		sizeTbl.RawSetString("type", lua.LString("window_size"))
		sizeTbl.RawSetString("width", lua.LNumber(msg.Width))
		sizeTbl.RawSetString("height", lua.LNumber(msg.Height))
		tbl.RawSetString("window_size", sizeTbl)

	default:
		tbl.RawSetString("go_type", lua.LString(reflect.TypeOf(msg).String()))
		tbl.RawSetString("string", lua.LString(fmt.Sprintf("%v", msg)))
	}
	return tbl
}

func LuaToMsg(value lua.LValue) (tea.Msg, error) {
	if tbl, ok := value.(*lua.LTable); ok {
		if opaque := tbl.RawGetString("opaque"); opaque != lua.LNil {
			if ud, ok := opaque.(*lua.LUserData); ok {
				if msg, ok := ud.Value.(tea.Msg); ok {
					return msg, nil
				}
			}
		}

		if key := tbl.RawGetString("key"); key != lua.LNil {
			return luaToKeyMsg(key.(*lua.LTable))
		}
		if mouse := tbl.RawGetString("mouse"); mouse != lua.LNil {
			return luaToMouseMsg(mouse.(*lua.LTable))
		}
		if size := tbl.RawGetString("window_size"); size != lua.LNil {
			return luaToWindowSizeMsg(size.(*lua.LTable))
		}
	}

	return value, nil
}

func luaToKeyMsg(tbl *lua.LTable) (tea.KeyMsg, error) {
	msg := tea.KeyMsg{
		Alt:   lua.LVAsBool(tbl.RawGetString("alt")),
		Paste: lua.LVAsBool(tbl.RawGetString("paste")),
		Runes: []rune(tbl.RawGetString("string").String()),
	}

	keyTypeStr := tbl.RawGetString("key_type").String()
	if keyType, ok := keyTypeFromStr[keyTypeStr]; ok {
		msg.Type = keyType
	} else {
		return msg, fmt.Errorf("unknown key type: %s", keyTypeStr)
	}

	return msg, nil
}

func luaToMouseMsg(tbl *lua.LTable) (tea.MouseMsg, error) {
	msg := tea.MouseMsg{
		X:     int(lua.LVAsNumber(tbl.RawGetString("x"))),
		Y:     int(lua.LVAsNumber(tbl.RawGetString("y"))),
		Alt:   lua.LVAsBool(tbl.RawGetString("alt")),
		Ctrl:  lua.LVAsBool(tbl.RawGetString("ctrl")),
		Shift: lua.LVAsBool(tbl.RawGetString("shift")),
	}

	switch tbl.RawGetString("action").String() {
	case "press":
		msg.Action = tea.MouseActionPress
	case "release":
		msg.Action = tea.MouseActionRelease
	case "motion":
		msg.Action = tea.MouseActionMotion
	}

	buttonStr := tbl.RawGetString("button").String()
	if button, ok := mouseButtonFromStr[buttonStr]; ok {
		msg.Button = button
	} else {
		return msg, fmt.Errorf("unknown mouse button: %s", buttonStr)
	}
	return msg, nil
}

func luaToWindowSizeMsg(tbl *lua.LTable) (tea.WindowSizeMsg, error) {
	return tea.WindowSizeMsg{
		Width:  int(lua.LVAsNumber(tbl.RawGetString("width"))),
		Height: int(lua.LVAsNumber(tbl.RawGetString("height"))),
	}, nil
}
