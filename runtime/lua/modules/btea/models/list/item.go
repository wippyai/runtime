package list

import (
	lua "github.com/yuin/gopher-lua"
	"reflect"
)

// TryGetMethodValue attempts to call a method on a Go value if it exists
func TryGetMethodValue(v interface{}, methodName string) (string, bool) {
	val := reflect.ValueOf(v)
	method := val.MethodByName(methodName)
	if !method.IsValid() {
		return "", false
	}

	ret := method.Call(nil)
	if len(ret) == 1 && ret[0].Kind() == reflect.String {
		return ret[0].String(), true
	}
	return "", false
}

// GetLuaField gets a field from any Lua value, handling both direct values and functions
func GetLuaField(l *lua.LState, value lua.LValue, field string) lua.LValue {
	// First try Go method if it's userdata
	if ud, ok := value.(*lua.LUserData); ok && ud.Value != nil {
		if str, ok := TryGetMethodValue(ud.Value, field); ok {
			return lua.LString(str)
		}
	}

	var fieldValue lua.LValue

	// Then try regular Lua value access
	switch v := value.(type) {
	case *lua.LTable:
		fieldValue = v.RawGetString(field)
	case *lua.LUserData:
		// Try metatable __index
		if mt := l.GetMetatable(value); mt != nil {
			if index, ok := mt.(*lua.LTable); ok {
				if indexVal := index.RawGetString("__index"); indexVal != lua.LNil {
					switch indexVal := indexVal.(type) {
					case *lua.LFunction:
						l.Push(indexVal)
						l.Push(value)
						l.Push(lua.LString(field))
						if err := l.PCall(2, 1, nil); err == nil {
							fieldValue = l.Get(-1)
							l.Pop(1)
						}
					case *lua.LTable:
						fieldValue = indexVal.RawGetString(field)
					}
				}
			}
		}
	}

	// If fieldValue is a function, call it with value as self
	if fn, ok := fieldValue.(*lua.LFunction); ok {
		l.Push(fn)
		l.Push(value)
		if err := l.PCall(1, 1, nil); err == nil {
			ret := l.Get(-1)
			l.Pop(1)
			return ret
		}
	}

	return fieldValue
}

// LuaItem wraps any Lua value to implement list.Item
type LuaItem struct {
	value    lua.LValue
	luaState *lua.LState
}

func NewLuaItem(l *lua.LState, value lua.LValue) *LuaItem {
	return &LuaItem{
		value:    value,
		luaState: l,
	}
}

func (li *LuaItem) FilterValue() string {
	if fv := GetLuaField(li.luaState, li.value, "FilterValue"); fv != lua.LNil {
		return lua.LVAsString(fv)
	}
	return ""
}

func (li *LuaItem) Title() string {
	if title := GetLuaField(li.luaState, li.value, "Title"); title != lua.LNil {
		return lua.LVAsString(title)
	}
	return ""
}

func (li *LuaItem) Description() string {
	if desc := GetLuaField(li.luaState, li.value, "Description"); desc != lua.LNil {
		return lua.LVAsString(desc)
	}
	return ""
}

// GetOriginalValue returns the underlying Lua value
func (li *LuaItem) GetOriginalValue() lua.LValue {
	return li.value
}
