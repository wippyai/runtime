package list

import (
	lua "github.com/yuin/gopher-lua"
)

// LuaItem is a wrapper to make Lua objects implement list.Item
type LuaItem struct {
	luaItem  *lua.LTable
	luaState *lua.LState
}

// Cached values to avoid excessive Lua calls
var (
	cachedFilterValues = make(map[*lua.LTable]string)
	cachedTitles       = make(map[*lua.LTable]string)
	cachedDescriptions = make(map[*lua.LTable]string)
)

func (li *LuaItem) FilterValue() string {
	if val, ok := cachedFilterValues[li.luaItem]; ok {
		return val
	}

	lv := li.luaItem.RawGetString("filter_value")
	if fn, ok := lv.(*lua.LFunction); ok {
		l := li.luaState
		if err := l.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, li.luaItem); err != nil {
			l.RaiseError("error calling filter_value: %v", err)
			return ""
		}
		ret := l.Get(-1)
		l.Pop(1)

		if ret.Type() == lua.LTString {
			filterVal := lua.LVAsString(ret)
			cachedFilterValues[li.luaItem] = filterVal
			return filterVal
		} else {
			l.RaiseError("filter_value must return a string")
		}
	}

	return ""
}

// --- Optional: For DefaultItem interface ---

func (li *LuaItem) Title() string {
	if val, ok := cachedTitles[li.luaItem]; ok {
		return val
	}

	lv := li.luaItem.RawGetString("title")
	if fn, ok := lv.(*lua.LFunction); ok {
		l := li.luaState
		if err := l.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, li.luaItem); err != nil {
			l.RaiseError("error calling title: %v", err)
			return ""
		}
		ret := l.Get(-1)
		l.Pop(1)

		if ret.Type() == lua.LTString {
			titleVal := lua.LVAsString(ret)
			cachedTitles[li.luaItem] = titleVal
			return titleVal
		} else {
			l.RaiseError("title must return a string")
		}
	}

	return ""
}

func (li *LuaItem) Description() string {
	if val, ok := cachedDescriptions[li.luaItem]; ok {
		return val
	}

	lv := li.luaItem.RawGetString("description")
	if fn, ok := lv.(*lua.LFunction); ok {
		l := li.luaState
		if err := l.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, li.luaItem); err != nil {
			l.RaiseError("error calling description: %v", err)
			return ""
		}
		ret := l.Get(-1)
		l.Pop(1)

		if ret.Type() == lua.LTString {
			descVal := lua.LVAsString(ret)
			cachedDescriptions[li.luaItem] = descVal
			return descVal
		} else {
			l.RaiseError("description must return a string")
		}
	}

	return ""
}

// Clear item caches when items are updated
func clearItemCaches(luaItem *lua.LTable) {
	delete(cachedFilterValues, luaItem)
	delete(cachedTitles, luaItem)
	delete(cachedDescriptions, luaItem)
}
