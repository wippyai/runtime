package value

import (
	lua "github.com/yuin/gopher-lua"
)

// GetTypeMetatable retrieves a type's metatable directly from registry
// without using the heavy GetField mechanism
func GetTypeMetatable(L *lua.LState, typeName string) *lua.LTable {
	// Get registry table directly
	registry := L.Get(lua.RegistryIndex)
	regTable, ok := registry.(*lua.LTable)
	if !ok {
		return nil // Registry isn't a table (shouldn't happen)
	}

	// Get metatable with direct access
	mt := regTable.RawGetString(typeName)
	if table, ok := mt.(*lua.LTable); ok {
		return table
	}

	return nil // Metatable doesn't exist or isn't a table
}

// RegisterTypeMethods efficiently registers methods for a type with minimal overhead.
// It takes separate maps for metamethods and regular methods, either of which can be nil.
func RegisterTypeMethods(
	L *lua.LState,
	typeName string,
	metamethods map[string]lua.LGFunction,
	methods map[string]lua.LGFunction,
) *lua.LTable {
	// Get registry table directly and ensure it's a table
	registry := L.Get(lua.RegistryIndex)
	regTable, ok := registry.(*lua.LTable)
	if !ok {
		L.RaiseError("registry is not a table")
		return nil
	}

	// Check if metatable already exists
	existingMt := regTable.RawGetString(typeName)
	var mt *lua.LTable

	if existingMt != lua.LNil {
		if existing, ok := existingMt.(*lua.LTable); ok {
			// Metatable already exists, use it
			mt = existing
		} else {
			// Unexpected value in registry, create new metatable
			mt = L.CreateTable(0, len(metamethods)+1) // +1 for possible __index
		}
	} else {
		// Create new metatable with exact size
		mt = L.CreateTable(0, len(metamethods)+1) // +1 for possible __index
	}

	// Add metamethods directly to metatable
	for name, fn := range metamethods {
		mt.RawSetString(name, L.NewFunction(fn))
	}

	// Handle regular methods if any
	if len(methods) > 0 {
		// Check if __index already exists and is a table
		indexVal := mt.RawGetString("__index")
		var indexTable *lua.LTable

		if existing, ok := indexVal.(*lua.LTable); ok {
			// Use existing index table
			indexTable = existing
		} else {
			// Create a new methods table with exact size
			indexTable = L.CreateTable(0, len(methods))
			// Set __index to the methods table
			mt.RawSetString("__index", indexTable)
		}

		// Add all methods to methodsTable directly
		for name, fn := range methods {
			indexTable.RawSetString(name, L.NewFunction(fn))
		}
	}

	// Store metatable in registry directly
	regTable.RawSetString(typeName, mt)

	return mt
}

func RegisterMetamethods(L *lua.LState, typeName string, metamethods map[string]lua.LGFunction) *lua.LTable {
	return RegisterTypeMethods(L, typeName, metamethods, nil)
}

func RegisterMethods(L *lua.LState, typeName string, methods map[string]lua.LGFunction) *lua.LTable {
	return RegisterTypeMethods(L, typeName, nil, methods)
}

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
	// Spawn the field using standard field access
	if v, ok := GetField(L, value, field); ok {
		// Check if it's a function
		if fn, ok := v.(*lua.LFunction); ok {
			return fn, true
		}
	}
	return nil, false
}
