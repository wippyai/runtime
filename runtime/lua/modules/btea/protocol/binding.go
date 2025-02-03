package protocol

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	lua "github.com/yuin/gopher-lua"
)

// Binding wraps key.Binding for Lua
type Binding struct {
	binding key.Binding
}

// RegisterBinding registers the key binding functionality
func RegisterBinding(l *lua.LState, mod *lua.LTable) {
	// Create and register the binding metatable
	mt := l.NewTypeMetatable("btea.Binding")
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
					keys = append(keys, string(str))
				}
			})
		} else if str, ok := keysTable.(lua.LString); ok {
			keys = append(keys, string(str))
		}
	}

	// Get help info
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

	// Create the binding
	binding := key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(helpKey, helpDesc),
	)

	// Create userdata
	ud := l.NewUserData()
	ud.Value = &Binding{binding: binding}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Binding"))
	l.Push(ud)
	return 1
}

func checkBinding(l *lua.LState) *Binding {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Binding); ok {
		return v
	}
	l.ArgError(1, "binding expected")
	return nil
}

// Binding methods

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
		l.Push(lua.LBool(key.Matches(keyMsg, b.binding)))
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
	b.binding.SetEnabled(enabled)
	return 0
}

func bindingIsEnabled(l *lua.LState) int {
	b := checkBinding(l)
	if b == nil {
		return 0
	}
	l.Push(lua.LBool(b.binding.Enabled()))
	return 1
}

func bindingGetHelp(l *lua.LState) int {
	b := checkBinding(l)
	if b == nil {
		return 0
	}
	help := b.binding.Help()
	tbl := l.NewTable()
	tbl.RawSetString("key", lua.LString(help.Key))
	tbl.RawSetString("desc", lua.LString(help.Desc))
	l.Push(tbl)
	return 1
}

// Helper functions for Go code to convert between Lua and Go bindings

// ToGoBinding converts a Lua binding to a Go key.Binding
func ToGoBinding(v lua.LValue) (key.Binding, bool) {
	if ud, ok := v.(*lua.LUserData); ok {
		if b, ok := ud.Value.(*Binding); ok {
			return b.binding, true
		}
	}
	return key.Binding{}, false
}

// ToLuaBinding converts a Go key.Binding to a Lua binding
func ToLuaBinding(l *lua.LState, b key.Binding) lua.LValue {
	ud := l.NewUserData()
	ud.Value = &Binding{binding: b}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Binding"))
	return ud
}
