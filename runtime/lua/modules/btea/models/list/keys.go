package list

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/wippyai/runtime/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
)

// luaTableToKeyMap converts a Lua table to a list.KeyMap struct.
func luaTableToKeyMap(l *lua.LState, keysTable *lua.LTable) list.KeyMap {
	listKeys := list.DefaultKeyMap()

	keyMap := map[string]*key.Binding{
		"cursor_up":              &listKeys.CursorUp,
		"cursor_down":            &listKeys.CursorDown,
		"prev_page":              &listKeys.PrevPage,
		"next_page":              &listKeys.NextPage,
		"go_to_start":            &listKeys.GoToStart,
		"go_to_end":              &listKeys.GoToEnd,
		"filter":                 &listKeys.Filter,
		"clear_filter":           &listKeys.ClearFilter,
		"cancel_while_filtering": &listKeys.CancelWhileFiltering,
		"accept_while_filtering": &listKeys.AcceptWhileFiltering,
		"show_full_help":         &listKeys.ShowFullHelp,
		"close_full_help":        &listKeys.CloseFullHelp,
		"quit":                   &listKeys.Quit,
		"force_quit":             &listKeys.ForceQuit,
	}

	for keyName, keyBindingPtr := range keyMap {
		if keyValue := keysTable.RawGetString(keyName); keyValue.Type() == lua.LTUserData {
			if binding, ok := getKeyBindingFromUserData(l, keyValue); ok {
				*keyBindingPtr = binding
			}
		}
	}

	return listKeys
}

// getKeyBindingFromUserData extracts a key.Binding from a btea.KeyBinding userdata.
func getKeyBindingFromUserData(l *lua.LState, bindingUD lua.LValue) (key.Binding, bool) {
	ud, ok := bindingUD.(*lua.LUserData)
	if !ok {
		l.RaiseError("Expected a btea.KeyBinding userdata")
		return key.Binding{}, false
	}
	binding, ok := ud.Value.(*protocol.KeyBinding)
	if !ok {
		l.RaiseError("Expected a btea.KeyBinding userdata")
		return key.Binding{}, false
	}
	return binding.Binding, true
}
