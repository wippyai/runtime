package list

import (
	"fmt"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
	"io"
	"log"
)

// LuaDelegate is a wrapper to make Lua functions act as list.ItemDelegate
type LuaDelegate struct {
	luaDelegate lua.LValue
	luaState    *lua.LState
}

func (ld *LuaDelegate) Height() int {
	if height := GetLuaField(ld.luaState, ld.luaDelegate, "height"); height != lua.LNil {
		return int(lua.LVAsNumber(height))
	}
	return 0 // Default height
}

func (ld *LuaDelegate) Spacing() int {
	if spacing := GetLuaField(ld.luaState, ld.luaDelegate, "spacing"); spacing != lua.LNil {
		return int(lua.LVAsNumber(spacing))
	}
	return 0 // Default spacing
}

func (ld *LuaDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	wrappedModel := wrapModelForLua(ld.luaState, &m)
	wrappedItem := wrapItemForLua(ld.luaState, listItem)

	if render := GetLuaField(ld.luaState, ld.luaDelegate, "render"); render != lua.LNil {
		if err := ld.luaState.CallByParam(lua.P{
			Fn:      render.(*lua.LFunction),
			NRet:    1,
			Protect: true,
		}, wrappedModel, lua.LNumber(index), wrappedItem); err != nil {
			ld.luaState.RaiseError("error calling delegate render: %v", err)
			return
		}
		ret := ld.luaState.Get(-1)
		ld.luaState.Pop(1)

		if str, ok := ret.(lua.LString); ok {
			if _, err := fmt.Fprint(w, string(str)); err != nil {
				ld.luaState.RaiseError("error writing to output: %v", err)
			}
		} else {
			ld.luaState.RaiseError("render must return a string")
		}
	}
}

func (ld *LuaDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	log.Printf("got: `%+v`", msg)
	luaMsg := protocol.MsgToLua(msg)
	tv := luaMsg.(*lua.LTable)

	// todo: reuse?
	wrappedModel := wrapModelForLua(ld.luaState, m)
	// todo: this is weird

	// todo: make proper lua helpers, do not use string result for expected methods
	// todo: can be nil!
	if update := FetchMethod(ld.luaState, ld.luaDelegate, "update"); update != lua.LNil {

		log.Printf("converted: %v", tv.RawGetString("type"))

		// todo: check if this is func
		if err := ld.luaState.CallByParam(lua.P{
			Fn:      update.(*lua.LFunction),
			NRet:    1,
			Protect: true,
		}, luaMsg, wrappedModel); err != nil {
			ld.luaState.RaiseError("error calling delegate update: %v", err)
			return nil
		}

		ret := ld.luaState.Get(-1)
		ld.luaState.Pop(1)

		if ret == lua.LNil {
			return nil
		}
		return protocol.UnwrapCommand(ld.luaState, ret)
	}

	return nil
}

func (ld *LuaDelegate) ShortHelp() []key.Binding {
	if shortHelp := GetLuaField(ld.luaState, ld.luaDelegate, "short_help"); shortHelp != lua.LNil {
		if t, ok := shortHelp.(*lua.LTable); ok {
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
	if fullHelp := GetLuaField(ld.luaState, ld.luaDelegate, "full_help"); fullHelp != lua.LNil {
		if t, ok := fullHelp.(*lua.LTable); ok {
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
