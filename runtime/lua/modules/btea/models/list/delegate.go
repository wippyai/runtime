package list

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
)

// LuaDelegate is a wrapper to make Lua functions act as list.ItemDelegate
type LuaDelegate struct {
	luaDelegate lua.LValue
	luaState    *lua.LState
}

func (ld *LuaDelegate) Height() int {
	if ud, ok := ld.luaDelegate.(*lua.LUserData); ok {
		if delegate, ok := ud.Value.(interface{ Height() int }); ok {
			return delegate.Height()
		}
	}

	if fieldValue, ok := value.GetField(ld.luaState, ld.luaDelegate, "height"); ok {
		switch v := fieldValue.(type) {
		case lua.LNumber:
			return int(v)
		case *lua.LFunction:
			ld.luaState.Push(v)
			ld.luaState.Push(ld.luaDelegate) // self
			if err := ld.luaState.PCall(1, 1, nil); err == nil {
				ret := ld.luaState.Get(-1)
				ld.luaState.Pop(1)
				return int(lua.LVAsNumber(ret))
			}
		}
	}
	return 0
}

func (ld *LuaDelegate) Spacing() int {
	if ud, ok := ld.luaDelegate.(*lua.LUserData); ok {
		if delegate, ok := ud.Value.(interface{ Spacing() int }); ok {
			return delegate.Spacing()
		}
	}

	if fieldValue, ok := value.GetField(ld.luaState, ld.luaDelegate, "spacing"); ok {
		switch v := fieldValue.(type) {
		case lua.LNumber:
			return int(v)
		case *lua.LFunction:
			ld.luaState.Push(v)
			ld.luaState.Push(ld.luaDelegate) // self
			if err := ld.luaState.PCall(1, 1, nil); err == nil {
				ret := ld.luaState.Get(-1)
				ld.luaState.Pop(1)
				return int(lua.LVAsNumber(ret))
			}
		}
	}
	return 0
}

func (ld *LuaDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	render, ok := value.GetFunc(ld.luaState, ld.luaDelegate, "render")
	if !ok {
		return
	}

	if err := ld.luaState.CallByParam(lua.P{
		Fn:      render,
		NRet:    1,
		Protect: true,
	}, ld.luaDelegate,
		wrapModelForLua(ld.luaState, &m),
		lua.LNumber(index),
		wrapItemForLua(ld.luaState, listItem)); err != nil {
		ld.luaState.RaiseError("error calling delegate render: %v", err)
		return
	}

	ret := ld.luaState.Get(-1)
	ld.luaState.Pop(1)

	if str, ok := ret.(lua.LString); ok {
		_, _ = fmt.Fprint(w, string(str))
	}
}

func (ld *LuaDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	update, ok := value.GetFunc(ld.luaState, ld.luaDelegate, "update")
	if !ok {
		return nil
	}

	luaMsg := protocol.MsgToLua(msg)
	wrappedModel := wrapModelForLua(ld.luaState, m)

	if err := ld.luaState.CallByParam(lua.P{
		Fn:      update,
		NRet:    1,
		Protect: true,
	}, ld.luaDelegate, luaMsg, wrappedModel); err != nil {
		ld.luaState.RaiseError("error calling delegate update: %v", err)
		return nil
	}

	ret := ld.luaState.Get(-1)
	ld.luaState.Pop(1)

	if ret == lua.LNil {
		return nil
	}

	cmd, err := protocol.UnwrapCommand(ret)
	if err != nil {
		ld.luaState.RaiseError("error unwrapping command: %v", err)
		return nil
	}

	return cmd
}

func (ld *LuaDelegate) ShortHelp() []key.Binding {
	if ud, ok := ld.luaDelegate.(*lua.LUserData); ok {
		if delegate, ok := ud.Value.(interface{ ShortHelp() []key.Binding }); ok {
			return delegate.ShortHelp()
		}
	}

	if fieldValue, ok := value.GetField(ld.luaState, ld.luaDelegate, "short_help"); ok {
		if t, ok := fieldValue.(*lua.LTable); ok {
			bindings := make([]key.Binding, 0)
			t.ForEach(func(_, v lua.LValue) {
				if binding, ok := getKeyBindingFromUserData(ld.luaState, v); ok {
					bindings = append(bindings, binding)
				}
			})
			return bindings
		}
	}
	return nil
}

func (ld *LuaDelegate) FullHelp() [][]key.Binding {
	if ud, ok := ld.luaDelegate.(*lua.LUserData); ok {
		if delegate, ok := ud.Value.(interface{ FullHelp() [][]key.Binding }); ok {
			return delegate.FullHelp()
		}
	}

	if fieldValue, ok := value.GetField(ld.luaState, ld.luaDelegate, "full_help"); ok {
		if t, ok := fieldValue.(*lua.LTable); ok {
			groupedBindings := make([][]key.Binding, 0)
			t.ForEach(func(_, group lua.LValue) {
				if groupTable, ok := group.(*lua.LTable); ok {
					bindings := make([]key.Binding, 0)
					groupTable.ForEach(func(_, v lua.LValue) {
						if binding, ok := getKeyBindingFromUserData(ld.luaState, v); ok {
							bindings = append(bindings, binding)
						}
					})
					if len(bindings) > 0 {
						groupedBindings = append(groupedBindings, bindings)
					}
				}
			})
			return groupedBindings
		}
	}
	return nil
}

func wrapModelForLua(l *lua.LState, m *list.Model) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = &List{
		model:    *m,
		luaState: l,
	}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.List"))
	return ud
}

func wrapItemForLua(l *lua.LState, item list.Item) lua.LValue {
	if li, ok := item.(*LuaItem); ok {
		return li.value
	}
	return lua.LNil
}
