// model.go in protocol package
package protocol

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

type LuaModelWrapper struct {
	value lua.LValue
	l     *lua.LState
}

func (m *LuaModelWrapper) Init() tea.Cmd {
	if fn, ok := engine.GetField(m.l, m.value, "init"); ok {
		// Call Lua init if exists
		if err := m.l.CallByParam(lua.P{
			Fn:      fn.(*lua.LFunction),
			NRet:    1,
			Protect: true,
		}, m.value); err == nil {
			ret := m.l.Get(-1)
			m.l.Pop(1)
			return UnwrapCommand(m.l, ret)
		}
	}
	return nil
}

func (m *LuaModelWrapper) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if fn, ok := engine.GetField(m.l, m.value, "update"); ok {
		if err := m.l.CallByParam(lua.P{
			Fn:      fn.(*lua.LFunction),
			NRet:    2,
			Protect: true,
		}, m.value, MsgToLua(msg)); err == nil {
			cmd := m.l.Get(-1)
			model := m.l.Get(-2)
			m.l.Pop(2)

			// If model is userdata, try unwrapping
			if ud, ok := model.(*lua.LUserData); ok {
				if mdl, ok := ud.Value.(tea.Model); ok {
					return mdl, UnwrapCommand(m.l, cmd)
				}
			}

			// Otherwise wrap new lua model
			return &LuaModelWrapper{
				value: model,
				l:     m.l,
			}, UnwrapCommand(m.l, cmd)
		}
	}
	return m, nil
}

func (m *LuaModelWrapper) View() string {
	if fn, ok := engine.GetField(m.l, m.value, "view"); ok {
		if err := m.l.CallByParam(lua.P{
			Fn:      fn.(*lua.LFunction),
			NRet:    1,
			Protect: true,
		}, m.value); err == nil {
			ret := m.l.Get(-1)
			m.l.Pop(1)
			return lua.LVAsString(ret)
		}
	}
	return ""
}

// TryGetModel attempts to convert a Lua value to a tea.Model
func TryGetModel(l *lua.LState, v lua.LValue) (tea.Model, bool) {
	// Case 1: Direct Go model in userdata
	if ud, ok := v.(*lua.LUserData); ok {
		if model, ok := ud.Value.(tea.Model); ok {
			return model, true
		}
	}

	// Case 2: Lua table/userdata with model methods
	if _, ok := engine.GetField(l, v, "update"); ok {
		if _, ok := engine.GetField(l, v, "view"); ok {
			return &LuaModelWrapper{
				value: v,
				l:     l,
			}, true
		}
	}

	return nil, false
}

// WrapModel converts a tea.Model back to Lua value
func WrapModel(l *lua.LState, m tea.Model) lua.LValue {
	// Case 1: Already a Lua value
	if lv, ok := m.(lua.LValue); ok {
		return lv
	}

	// Case 2: Our wrapper - return original Lua value
	if m, ok := m.(*LuaModelWrapper); ok {
		return m.value
	}

	// Case 3: Go model - wrap in userdata
	ud := l.NewUserData()
	ud.Value = m
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Model"))
	return ud
}
