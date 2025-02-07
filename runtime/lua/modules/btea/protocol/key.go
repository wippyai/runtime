package protocol

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	lua "github.com/yuin/gopher-lua"
)

// KeyBinding wraps key.Binding for Lua
type KeyBinding struct {
	Binding key.Binding
}

// RegisterKeyBinding registers the key KeyBinding functionality
func RegisterKeyBinding(l *lua.LState, mod *lua.LTable) {
	// Create and register the KeyBinding metatable
	mt := l.NewTypeMetatable("btea.KeyBinding")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"set_enabled": bindingSetEnabled,
		"is_enabled":  bindingIsEnabled,
		"help":        bindingGetHelp,
		"matches":     bindingMatches,
	}))

	// Register constructor
	l.SetField(mod, "new_binding", l.NewFunction(newBinding))
}

func newBinding(l *lua.LState) int {
	opts := l.CheckTable(1)

	// Get keys from the table
	var keys []string
	if keysTable := opts.RawGetString("keys"); keysTable != lua.LNil {
		if t, ok := keysTable.(*lua.LTable); ok {
			t.ForEach(func(_, v lua.LValue) {
				if str, ok := v.(lua.LString); ok {
					k := string(str)
					keys = append(keys, k)
					// Special case: when "space" is specified, also add the actual space character
					if k == "space" {
						keys = append(keys, " ")
					}
				}
			})
		} else if str, ok := keysTable.(lua.LString); ok {
			k := string(str)
			keys = append(keys, k)
			// Handle single string case for space as well
			if k == "space" {
				keys = append(keys, " ")
			}
		}
	}

	// Rest of the code remains the same...
	var helpKey, helpDesc string
	if helpTable := opts.RawGetString("help"); helpTable != lua.LNil {
		if t, ok := helpTable.(*lua.LTable); ok {
			if k := t.RawGetString("key"); k != lua.LNil {
				helpKey = lua.LVAsString(k)
			}
			if d := t.RawGetString("desc"); d != lua.LNil {
				helpDesc = lua.LVAsString(d)
			}
		}
	}

	// Create the KeyBinding
	binding := key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(helpKey, helpDesc),
	)

	// Create userdata
	ud := l.NewUserData()
	ud.Value = &KeyBinding{Binding: binding}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.KeyBinding"))
	l.Push(ud)
	return 1
}

func checkBinding(l *lua.LState) *KeyBinding {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*KeyBinding); ok {
		return v
	}
	l.ArgError(1, "KeyBinding expected")
	return nil
}

// KeyBinding methods

func bindingMatches(l *lua.LState) int {
	b := checkBinding(l)
	if b == nil {
		return 0
	}

	// Get message argument and convert to tea.Msg
	msgValue := l.CheckAny(2)
	msg, err := LuaToMsg(msgValue)

	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}

	// If it's a KeyMsg, check if it matches
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		l.Push(lua.LBool(key.Matches(keyMsg, b.Binding)))
		return 1
	}

	l.Push(lua.LBool(false))
	return 1
}

func bindingSetEnabled(l *lua.LState) int {
	b := checkBinding(l)
	if b == nil {
		return 0
	}
	enabled := l.CheckBool(2)
	b.Binding.SetEnabled(enabled)
	return 0
}

func bindingIsEnabled(l *lua.LState) int {
	b := checkBinding(l)
	if b == nil {
		return 0
	}
	l.Push(lua.LBool(b.Binding.Enabled()))
	return 1
}

func bindingGetHelp(l *lua.LState) int {
	b := checkBinding(l)
	if b == nil {
		return 0
	}
	help := b.Binding.Help()
	tbl := l.NewTable()
	tbl.RawSetString("key", lua.LString(help.Key))
	tbl.RawSetString("desc", lua.LString(help.Desc))
	l.Push(tbl)
	return 1
}

// Helper functions for Go code to convert between Lua and Go bindings

// ToGoKeyBinding converts a Lua KeyBinding to a Go key.Binding
func ToGoKeyBinding(v lua.LValue) (key.Binding, bool) {
	if ud, ok := v.(*lua.LUserData); ok {
		if b, ok := ud.Value.(*KeyBinding); ok {
			return b.Binding, true
		}
	}
	return key.Binding{}, false
}

// ToLuaKeyBinding converts a Go key.Binding to a Lua KeyBinding
func ToLuaKeyBinding(l *lua.LState, b key.Binding) lua.LValue {
	ud := l.NewUserData()
	ud.Value = &KeyBinding{Binding: b}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.KeyBinding"))
	return ud
}
