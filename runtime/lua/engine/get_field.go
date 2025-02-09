package engine

import (
	lua "github.com/yuin/gopher-lua"
)

// GetField retrieves a field value from a Lua value following Lua's field access rules.
// It implements the complete field lookup semantics including metatables:
//
//  1. If the value is a table, attempts direct field access
//  2. If the field is not found or the value is not a table (including userdata),
//     and the value has a metatable with __index:
//     - If __index is a function, calls it with (value, field)
//     - If __index is a table, looks up the field in that table
//
// This function properly handles all Lua types including:
// - Tables (direct access + metamethods)
// - Userdata (metamethods)
// - Strings (metamethods)
// - Numbers (metamethods)
// - Booleans (metamethods)
// - Nil (always returns nil, false)
//
// Parameters:
//   - L: The Lua state
//   - value: The Lua value to get the field from
//   - field: The field name to look up
//
// Returns:
//   - The field value and true if found
//   - lua.LNil and false if not found or on any error
//
// This function never panics and safely handles all error conditions.
func GetField(L *lua.LState, value lua.LValue, field string) (lua.LValue, bool) {
	// Direct table access first (most common case)
	if table, ok := value.(*lua.LTable); ok {
		v := table.RawGetString(field)
		if v != lua.LNil {
			return v, true
		}
	}

	// Check metatable for __index (works for both tables and userdata)
	if mt := L.GetMetatable(value); mt != nil {
		mtTable, ok := mt.(*lua.LTable)
		if !ok {
			return lua.LNil, false
		}

		index := mtTable.RawGetString("__index")
		if index == lua.LNil {
			return lua.LNil, false
		}

		switch index.Type() {
		case lua.LTFunction:
			L.Push(index)
			L.Push(value)
			L.Push(lua.LString(field))
			if err := L.PCall(2, 1, nil); err == nil {
				ret := L.Get(-1)
				L.Pop(1)
				return ret, ret != lua.LNil
			}
			return lua.LNil, false

		case lua.LTTable:
			if indexTable, ok := index.(*lua.LTable); ok {
				v := indexTable.RawGetString(field)
				return v, v != lua.LNil
			}
			return lua.LNil, false

		default:
			// Any other __index type is invalid according to Lua spec
			return lua.LNil, false
		}
	}

	return lua.LNil, false
}

// GetFunc retrieves a function from a Lua value following Lua's field access rules.
// It works similar to GetField but specifically returns only function.
//
// The lookup follows these steps:
//  1. If the value is a table, attempts direct field access
//  2. If the field is not found or value is not a table (including userdata),
//     and the value has a metatable with __index:
//     - If __index is a function, calls it with (value, field)
//     - If __index is a table, looks up the field in that table
//
// Parameters:
//   - L: The Lua state
//   - value: The Lua value to get the function from
//   - field: The field name to look up
//
// Returns:
//   - The function and true if found and is a function
//   - nil and false if not found or value is not a function
//
// This function never panics and safely handles all error conditions.
func GetFunc(L *lua.LState, value lua.LValue, field string) (*lua.LFunction, bool) {
	// Create the field using standard field access
	if v, ok := GetField(L, value, field); ok {
		// Check if it's a function
		if fn, ok := v.(*lua.LFunction); ok {
			return fn, true
		}
	}
	return nil, false
}
