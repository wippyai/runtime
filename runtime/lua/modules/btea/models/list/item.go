package list

import (
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// LuaItem wraps any Lua value to implement list.Item
type LuaItem struct {
	value    lua.LValue
	luaState *lua.LState
}

func (li *LuaItem) FilterValue() string {
	// First check if value directly implements FilterValue
	if ud, ok := li.value.(*lua.LUserData); ok {
		if item, ok := ud.Value.(interface{ FilterValue() string }); ok {
			return item.FilterValue()
		}
	}

	if fieldValue, ok := value.GetField(li.luaState, li.value, "filter_value"); ok {
		switch v := fieldValue.(type) {
		case lua.LString:
			return string(v)
		case *lua.LFunction:
			li.luaState.Push(v)
			li.luaState.Push(li.value) // self
			if err := li.luaState.PCall(1, 1, nil); err == nil {
				ret := li.luaState.Get(-1)
				li.luaState.Pop(1)
				return lua.LVAsString(ret)
			}
		}
	}
	return ""
}

func (li *LuaItem) Title() string {
	// First check if value directly implements Title
	if ud, ok := li.value.(*lua.LUserData); ok {
		if item, ok := ud.Value.(interface{ Title() string }); ok {
			return item.Title()
		}
	}

	if fieldValue, ok := value.GetField(li.luaState, li.value, "title"); ok {
		switch v := fieldValue.(type) {
		case lua.LString:
			return string(v)
		case *lua.LFunction:
			li.luaState.Push(v)
			li.luaState.Push(li.value) // self
			if err := li.luaState.PCall(1, 1, nil); err == nil {
				ret := li.luaState.Get(-1)
				li.luaState.Pop(1)
				return lua.LVAsString(ret)
			}
		}
	}
	return ""
}

func (li *LuaItem) Description() string {
	// First check if value directly implements Description
	if ud, ok := li.value.(*lua.LUserData); ok {
		if item, ok := ud.Value.(interface{ Description() string }); ok {
			return item.Description()
		}
	}

	if fieldValue, ok := value.GetField(li.luaState, li.value, "description"); ok {
		switch v := fieldValue.(type) {
		case lua.LString:
			return string(v)
		case *lua.LFunction:
			li.luaState.Push(v)
			li.luaState.Push(li.value) // self
			if err := li.luaState.PCall(1, 1, nil); err == nil {
				ret := li.luaState.Get(-1)
				li.luaState.Pop(1)
				return lua.LVAsString(ret)
			}
		}
	}
	return ""
}

// GetOriginalValue returns the underlying Lua value
func (li *LuaItem) GetOriginalValue() lua.LValue {
	return li.value
}
