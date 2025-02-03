package list

import (
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
)

// LuaDelegate is a wrapper to make Lua functions act as list.ItemDelegate
type LuaDelegate struct {
	luaDelegate *lua.LFunction
	luaState    *lua.LState
}

func (ld *LuaDelegate) Height() int {
	l := ld.luaState
	if err := l.CallByParam(lua.P{
		Fn:      ld.luaDelegate,
		NRet:    1,
		Protect: true,
	}, lua.LString("height")); err != nil {
		l.RaiseError("error calling delegate height: %v", err)
		return 0 // Default height
	}
	ret := l.Get(-1)
	l.Pop(1)

	return int(lua.LVAsNumber(ret))
}

func (ld *LuaDelegate) Spacing() int {
	l := ld.luaState
	if err := l.CallByParam(lua.P{
		Fn:      ld.luaDelegate,
		NRet:    1,
		Protect: true,
	}, lua.LString("spacing")); err != nil {
		l.RaiseError("error calling delegate spacing: %v", err)
		return 0 // Default spacing
	}
	ret := l.Get(-1)
	l.Pop(1)

	return int(lua.LVAsNumber(ret))
}

// LuaListWriter is used to capture output from Go to Lua strings.
type LuaListWriter struct {
	L     *lua.LState
	parts []string
}

// Write appends byte slices to the writer's parts.
func (w *LuaListWriter) Write(p []byte) (int, error) {
	w.parts = append(w.parts, string(p))
	return len(p), nil
}

// String joins all parts into a single string.
func (w *LuaListWriter) String() string {
	return strings.Join(w.parts, "")
}

func (ld *LuaDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	l := ld.luaState

	wrappedModel := wrapModelForLua(l, &m)
	wrappedItem := wrapItemForLua(l, listItem)

	lw, ok := w.(*LuaListWriter)
	if !ok {
		l.RaiseError("io.Writer is not a LuaListWriter")
		return
	}

	if err := l.CallByParam(lua.P{
		Fn:      ld.luaDelegate,
		NRet:    0,
		Protect: true,
	}, lua.LString("render"), lw, wrappedModel, lua.LNumber(index), wrappedItem); err != nil {
		l.RaiseError("error calling delegate render: %v", err)
	}
}

func (ld *LuaDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	l := ld.luaState
	luaMsg := protocol.MsgToLua(msg)

	// Wrap the model for Lua
	wrappedModel := wrapModelForLua(l, m)

	if err := l.CallByParam(lua.P{
		Fn:      ld.luaDelegate,
		NRet:    1,
		Protect: true,
	}, lua.LString("update"), luaMsg, wrappedModel); err != nil {
		l.RaiseError("error calling delegate update: %v", err)
		return nil
	}

	ret := l.Get(-1)
	l.Pop(1)

	if ret == lua.LNil {
		return nil
	}

	// Convert Lua command back to tea.Cmd
	cmd := protocol.UnwrapCommand(l, ret)
	return cmd
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

func wrapItemForLua(l *lua.LState, item list.Item) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = item
	l.SetMetatable(ud, l.GetTypeMetatable("btea.ListItem"))
	return ud
}
