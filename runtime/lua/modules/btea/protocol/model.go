package protocol

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"log"
	"reflect"
)

// LuaModelWrapper wraps either userdata or table to implement tea.Model
type LuaModelWrapper struct {
	value    lua.LValue
	luaState *lua.LState
}

func (m *LuaModelWrapper) Init() tea.Cmd {
	if fn, ok := engine.GetFunc(m.luaState, m.value, "init"); ok {
		err := m.luaState.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, m.value)
		if err == nil {
			ret := m.luaState.Get(-1)
			m.luaState.Pop(1)
			return UnwrapCommand(m.luaState, ret)
		}
	}
	return nil
}

func (m *LuaModelWrapper) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if fn, ok := engine.GetFunc(m.luaState, m.value, "update"); ok {
		mouseMsg, ok := msg.(tea.MouseMsg)
		if ok {
			log.Printf("MOUSE MSG: %v", mouseMsg)
		}

		luaMsg := MsgToLua(msg)

		err := m.luaState.CallByParam(lua.P{
			Fn:      fn,
			NRet:    2,
			Protect: true,
		}, m.value, luaMsg)

		if err == nil {
			cmdRet := m.luaState.Get(-1)
			modelRet := m.luaState.Get(-2)
			m.luaState.Pop(2)

			// Handle potential new model value
			if modelRet != lua.LNil {
				m.value = modelRet
			}

			return m, UnwrapCommand(m.luaState, cmdRet)
		}
	}
	return m, nil
}

func (m *LuaModelWrapper) View() string {
	if fn, ok := engine.GetFunc(m.luaState, m.value, "view"); ok {
		err := m.luaState.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, m.value)
		if err == nil {
			ret := m.luaState.Get(-1)
			m.luaState.Pop(1)
			if str, ok := ret.(lua.LString); ok {
				return string(str)
			}
		}
	}
	return ""
}

// TryGetModel attempts to extract or wrap a tea.Model from a Lua value
func TryGetModel(l *lua.LState, v lua.LValue) (tea.Model, bool) {
	// Direct tea.Model implementation
	if model, ok := v.(tea.Model); ok {
		return model, true
	}

	// Check userdata wrapping tea.Model
	if ud, ok := v.(*lua.LUserData); ok {
		if model, ok := ud.Value.(tea.Model); ok {
			return model, true
		}
	}

	// For both userdata and tables, try wrapping if they have model methods
	_, hasInit := engine.GetFunc(l, v, "init")
	_, hasUpdate := engine.GetFunc(l, v, "update")
	_, hasView := engine.GetFunc(l, v, "view")

	if hasInit && hasUpdate && hasView {
		return &LuaModelWrapper{value: v, luaState: l}, true
	}

	return nil, false
}

// UpdateModelValue handles updating model state
func UpdateModelValue(l *lua.LState, v lua.LValue, newModel tea.Model) bool {
	ud, ok := v.(*lua.LUserData)
	if !ok {
		return false
	}

	currentModel, ok := ud.Value.(tea.Model)
	if !ok {
		return false
	}

	// Handle wrapped models
	if wrapper, ok := currentModel.(*LuaModelWrapper); ok {
		if newWrapper, ok := newModel.(*LuaModelWrapper); ok {
			wrapper.value = newWrapper.value
			return true
		}
		return false
	}

	// Handle direct models
	if reflect.TypeOf(currentModel) == reflect.TypeOf(newModel) {
		ud.Value = newModel
		return true
	}

	return false
}
