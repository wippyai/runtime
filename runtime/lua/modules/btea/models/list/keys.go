package list

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
)

// luaTableToKeyMap converts a Lua table to a list.KeyMap struct.
func luaTableToKeyMap(l *lua.LState, keys *lua.LTable) list.KeyMap {
	listKeys := list.DefaultKeyMap()

	if cursorUp := keys.RawGetString("cursor_up"); cursorUp.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, cursorUp); ok {
			listKeys.CursorUp = binding
		}
	}
	if cursorDown := keys.RawGetString("cursor_down"); cursorDown.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, cursorDown); ok {
			listKeys.CursorDown = binding
		}
	}
	if prevPage := keys.RawGetString("prev_page"); prevPage.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, prevPage); ok {
			listKeys.PrevPage = binding
		}
	}
	if nextPage := keys.RawGetString("next_page"); nextPage.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, nextPage); ok {
			listKeys.NextPage = binding
		}
	}
	if goToStart := keys.RawGetString("go_to_start"); goToStart.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, goToStart); ok {
			listKeys.GoToStart = binding
		}
	}
	if goToEnd := keys.RawGetString("go_to_end"); goToEnd.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, goToEnd); ok {
			listKeys.GoToEnd = binding
		}
	}
	if filter := keys.RawGetString("filter"); filter.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, filter); ok {
			listKeys.Filter = binding
		}
	}
	if clearFilter := keys.RawGetString("clear_filter"); clearFilter.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, clearFilter); ok {
			listKeys.ClearFilter = binding
		}
	}
	if cancelWhileFiltering := keys.RawGetString("cancel_while_filtering"); cancelWhileFiltering.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, cancelWhileFiltering); ok {
			listKeys.CancelWhileFiltering = binding
		}
	}
	if acceptWhileFiltering := keys.RawGetString("accept_while_filtering"); acceptWhileFiltering.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, acceptWhileFiltering); ok {
			listKeys.AcceptWhileFiltering = binding
		}
	}
	if showFullHelp := keys.RawGetString("show_full_help"); showFullHelp.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, showFullHelp); ok {
			listKeys.ShowFullHelp = binding
		}
	}
	if closeFullHelp := keys.RawGetString("close_full_help"); closeFullHelp.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, closeFullHelp); ok {
			listKeys.CloseFullHelp = binding
		}
	}
	if quit := keys.RawGetString("quit"); quit.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, quit); ok {
			listKeys.Quit = binding
		}
	}
	if forceQuit := keys.RawGetString("force_quit"); forceQuit.Type() == lua.LTUserData {
		if binding, ok := getKeyBindingFromUserData(l, forceQuit); ok {
			listKeys.ForceQuit = binding
		}
	}

	return listKeys
}

// getKeyBindingFromUserData extracts a key.Binding from a btea.KeyBinding userdata.
func getKeyBindingFromUserData(l *lua.LState, bindingUD lua.LValue) (key.Binding, bool) {
	if ud, ok := bindingUD.(*lua.LUserData); ok {
		if binding, ok := ud.Value.(*protocol.KeyBinding); ok {
			return binding.Binding, true
		}
	}
	l.RaiseError("Expected a btea.KeyBinding userdata")
	return key.Binding{}, false
}
