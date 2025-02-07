package engine

import (
	lua "github.com/yuin/gopher-lua"
)

func GetField(L *lua.LState, value lua.LValue, field string) (lua.LValue, bool) {
	// Direct table access
	if table, ok := value.(*lua.LTable); ok {
		v := table.RawGetString(field)
		if v != lua.LNil {
			return v, true
		}
	}

	// Check metatable for __index
	if mt := L.GetMetatable(value); mt != nil {
		// Safely check if metatable is a table
		mtTable, ok := mt.(*lua.LTable)
		if !ok {
			return lua.LNil, false
		}

		// Get __index
		index := mtTable.RawGetString("__index")
		if index == lua.LNil {
			return lua.LNil, false
		}

		// Handle __index based on type
		switch index.Type() {
		case lua.LTFunction:
			// Call __index function
			L.Push(index)
			L.Push(value)
			L.Push(lua.LString(field))
			if err := L.PCall(2, 1, nil); err == nil {
				ret := L.Get(-1)
				L.Pop(1)
				return ret, ret != lua.LNil
			}
			// If function call fails, return nil
			return lua.LNil, false

		case lua.LTTable:
			// Safely convert to table
			if indexTable, ok := index.(*lua.LTable); ok {
				v := indexTable.RawGetString(field)
				return v, v != lua.LNil
			}
		}
	}

	return lua.LNil, false
}
