// Package luafield provides basic field access for Lua values
package luafield

import (
	"fmt"
	lua "github.com/yuin/gopher-lua"
)

// Get retrieves a field from any Lua value
// Returns (value, true) if found, (nil, false) if not found
func Get(L *lua.LState, value lua.LValue, field string) (lua.LValue, bool) {
	// Direct table access
	if table, ok := value.(*lua.LTable); ok {
		v := table.RawGetString(field)
		return v, v != lua.LNil
	}

	// Check metatable for __index
	if mt := L.GetMetatable(value); mt != nil {
		if t, ok := mt.(*lua.LTable); ok {
			if index := t.RawGetString("__index"); index != lua.LNil {
				switch v := index.(type) {
				case *lua.LFunction:
					L.Push(v)
					L.Push(value)
					L.Push(lua.LString(field))
					if err := L.PCall(2, 1, nil); err == nil {
						ret := L.Get(-1)
						L.Pop(1)
						return ret, ret != lua.LNil
					}
				case *lua.LTable:
					v := v.RawGetString(field)
					return v, v != lua.LNil
				}
			}
		}
	}

	return lua.LNil, false
}

// Call attempts to call a function with args
// Returns (value, true, nil) if successful, (nil, false, error) if failed
func Call(L *lua.LState, fn lua.LValue, args ...lua.LValue) (lua.LValue, bool, error) {
	if fn.Type() != lua.LTFunction {
		return lua.LNil, false, fmt.Errorf("not a function")
	}

	L.Push(fn)
	for _, arg := range args {
		L.Push(arg)
	}

	if err := L.PCall(len(args), 1, nil); err != nil {
		return lua.LNil, false, err
	}

	ret := L.Get(-1)
	L.Pop(1)
	return ret, true, nil
}
