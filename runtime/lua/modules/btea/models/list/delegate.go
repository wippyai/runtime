package list

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	lua "github.com/yuin/gopher-lua"
)

// LuaDelegate is a wrapper to make Lua functions act as list.ItemDelegate
type LuaDelegate struct {
	luaDelegate lua.LValue
	luaState    *lua.LState
}

func (ld *LuaDelegate) Height() int {
	l := ld.luaState
	fn := l.GetField(ld.luaDelegate, "height")
	if fn.Type() != lua.LTFunction {
		return 0 // Default height
	}

	if err := l.CallByParam(lua.P{
		Fn:      fn.(*lua.LFunction),
		NRet:    1,
		Protect: true,
	}); err != nil {
		l.RaiseError("error calling delegate height: %v", err)
		return 0
	}
	ret := l.Get(-1)
	l.Pop(1)
	return int(lua.LVAsNumber(ret))
}

func (ld *LuaDelegate) Spacing() int {
	l := ld.luaState
	fn := l.GetField(ld.luaDelegate, "spacing")
	if fn.Type() != lua.LTFunction {
		return 0 // Default spacing
	}

	if err := l.CallByParam(lua.P{
		Fn:      fn.(*lua.LFunction),
		NRet:    1,
		Protect: true,
	}); err != nil {
		l.RaiseError("error calling delegate spacing: %v", err)
		return 0
	}
	ret := l.Get(-1)
	l.Pop(1)
	return int(lua.LVAsNumber(ret))
}

func (ld *LuaDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	l := ld.luaState
	fn := l.GetField(ld.luaDelegate, "render")
	if fn.Type() != lua.LTFunction {
		return
	}

	wrappedModel := wrapModelForLua(l, &m)
	wrappedItem := wrapItemForLua(l, listItem)
	if err := l.CallByParam(lua.P{
		Fn:      fn.(*lua.LFunction),
		NRet:    1,
		Protect: true,
	}, wrappedModel, lua.LNumber(index), wrappedItem); err != nil {
		l.RaiseError("error calling delegate render: %v", err)
		return
	}
	ret := l.Get(-1)
	l.Pop(1)

	if str, ok := ret.(lua.LString); ok {
		_, err := fmt.Fprint(w, string(str))
		if err != nil {
			l.RaiseError("error writing to output: %v", err)
		}
	} else {
		l.RaiseError("render must return a string")
	}
}

func (ld *LuaDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	l := ld.luaState
	fn := l.GetField(ld.luaDelegate, "update")
	if fn.Type() != lua.LTFunction {
		return nil
	}

	luaMsg := protocol.MsgToLua(msg)
	wrappedModel := wrapModelForLua(l, m)
	if err := l.CallByParam(lua.P{
		Fn:      fn.(*lua.LFunction),
		NRet:    1,
		Protect: true,
	}, luaMsg, wrappedModel); err != nil {
		l.RaiseError("error calling delegate update: %v", err)
		return nil
	}
	ret := l.Get(-1)
	l.Pop(1)

	if ret == lua.LNil {
		return nil
	}
	return protocol.UnwrapCommand(l, ret)
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

	// Fallback for non-LuaItems
	return lua.LNil
}
