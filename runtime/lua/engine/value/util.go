package value

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// Global metatable storage using sync.Map for fast concurrent access
// All stored metatables are made immutable for safe reuse across goroutines
var metatableRegistry sync.Map

// IsTypeRegistered checks if a type metatable is already registered and immutable
func IsTypeRegistered(typeName string) bool {
	if mt, ok := metatableRegistry.Load(typeName); ok {
		if table, ok := mt.(*lua.LTable); ok {
			return table.Immutable
		}
	}
	return false
}

// GetTypeMetatable retrieves a type's metatable from internal storage
// without touching the Lua state registry. Returns the shared immutable metatable.
func GetTypeMetatable(_ *lua.LState, typeName string) *lua.LTable {
	if mt, ok := metatableRegistry.Load(typeName); ok {
		if table, ok := mt.(*lua.LTable); ok {
			return table
		}
	}
	return nil
}

// RegisterTypeMethods efficiently registers methods for a type with minimal overhead.
// It stores metatables in internal sync.Map instead of polluting Lua state.
// Takes separate maps for metamethods and regular methods, either can be nil.
//
// Functions are stored as LGoFunc directly (zero allocation) and the immutable metatables
// are safely reused across all LStates since Go functions don't depend on environment.
// If a metatable already exists and is immutable, attempting to add new methods
// will create a new metatable (this allows for incremental registration).
func RegisterTypeMethods(
	_ *lua.LState, // Not used, kept for API compatibility
	typeName string,
	metamethods map[string]lua.LGFunction,
	methods map[string]lua.LGFunction,
) *lua.LTable {
	// Check if metatable already exists in our registry
	var mt *lua.LTable
	var shouldCreateNew = false

	if existing, ok := metatableRegistry.Load(typeName); ok {
		if existingMt, ok := existing.(*lua.LTable); ok {
			// If existing metatable is immutable, we need to create a new one
			// to add new methods/metamethods
			if existingMt.Immutable && (len(metamethods) > 0 || len(methods) > 0) {
				shouldCreateNew = true
			} else {
				mt = existingMt
			}
		}
	} else {
		shouldCreateNew = true
	}

	// Create new metatable if needed
	if shouldCreateNew {
		// Create metatable with exact size (+1 for possible __index)
		totalSize := len(metamethods)
		if len(methods) > 0 {
			totalSize++ // for __index
		}
		mt = lua.CreateTable(0, totalSize)
	}

	// Add metamethods directly to metatable using LGoFunc (zero allocation)
	if !mt.Immutable {
		for name, fn := range metamethods {
			mt.RawSetString(name, lua.LGoFunc(fn))
		}
	} else if len(metamethods) > 0 {
		// This should not happen due to shouldCreateNew logic above,
		// but guard against it for safety
		panic("attempting to modify immutable metatable")
	}

	// Handle regular methods if any (only if not immutable)
	if len(methods) > 0 && !mt.Immutable {
		// Check if __index already exists and is a table
		indexVal := mt.RawGetString("__index")
		var indexTable *lua.LTable

		if existing, ok := indexVal.(*lua.LTable); ok {
			// Use existing index table
			indexTable = existing
		} else {
			// Create new methods table with exact size
			indexTable = lua.CreateTable(0, len(methods))
			// Set __index to the methods table
			mt.RawSetString("__index", indexTable)
		}

		// Add all methods to indexTable using LGoFunc (zero allocation)
		for name, fn := range methods {
			indexTable.RawSetString(name, lua.LGoFunc(fn))
		}

		// Make the index table immutable for safe reuse
		indexTable.Immutable = true
	} else if len(methods) > 0 {
		// This should not happen due to shouldCreateNew logic above,
		// but guard against it for safety
		panic("attempting to add methods to immutable metatable")
	}

	// Make the metatable immutable for safe reuse across goroutines and Lua states
	if !mt.Immutable {
		mt.Immutable = true
	}

	// Store metatable in our internal registry
	metatableRegistry.Store(typeName, mt)

	return mt
}

// RegisterMetamethods registers only metamethods for a type
func RegisterMetamethods(l *lua.LState, typeName string, metamethods map[string]lua.LGFunction) *lua.LTable {
	return RegisterTypeMethods(l, typeName, metamethods, nil)
}

// RegisterMethods registers only regular methods for a type
func RegisterMethods(l *lua.LState, typeName string, methods map[string]lua.LGFunction) *lua.LTable {
	return RegisterTypeMethods(l, typeName, nil, methods)
}

// PushUserData creates a new userdata with the given value and metatable,
// pushes it onto the stack, and returns it.
func PushUserData(l *lua.LState, val any, metatable *lua.LTable) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = val
	ud.Metatable = metatable
	l.Push(ud)
	return ud
}

// PushTypedUserData creates a new userdata with the given value, looks up
// the metatable by type name from the registry, pushes it onto the stack,
// and returns it. Returns nil if the type is not registered.
func PushTypedUserData(l *lua.LState, val any, typeName string) *lua.LUserData {
	mt := GetTypeMetatable(l, typeName)
	if mt == nil {
		return nil
	}
	return PushUserData(l, val, mt)
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
// This function properly handles all Lua types and never panics.
func GetField(l *lua.LState, value lua.LValue, field string) (lua.LValue, bool) {
	// Fast path: direct table access (most common case)
	if table, ok := value.(*lua.LTable); ok {
		v := table.RawGetString(field)
		if v != lua.LNil {
			return v, true
		}
	}

	// Check metatable for __index (works for tables and userdata)
	if mt := l.GetMetatable(value); mt != nil {
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
			// Call __index function
			l.Push(index)
			l.Push(value)
			l.Push(lua.LString(field))
			if err := l.PCall(2, 1, nil); err == nil {
				ret := l.Get(-1)
				l.Pop(1)
				return ret, ret != lua.LNil
			}
			return lua.LNil, false

		case lua.LTTable:
			// Look up in __index table
			if indexTable, ok := index.(*lua.LTable); ok {
				v := indexTable.RawGetString(field)
				return v, v != lua.LNil
			}
			return lua.LNil, false

		case lua.LTNil, lua.LTBool, lua.LTNumber, lua.LTString, lua.LTUserData, lua.LTThread, lua.LTChannel:
			// Invalid __index type according to Lua spec
			return lua.LNil, false
		}
	}

	return lua.LNil, false
}

// GetFunc retrieves a function from a Lua value following Lua's field access rules.
// Returns the function and true if found and is a function, nil and false otherwise.
func GetFunc(l *lua.LState, value lua.LValue, field string) (*lua.LFunction, bool) {
	if v, ok := GetField(l, value, field); ok {
		if fn, ok := v.(*lua.LFunction); ok {
			return fn, true
		}
	}
	return nil, false
}

// ToGoAny converts a lua.LValue to its Go equivalent.
func ToGoAny(v lua.LValue) any {
	if v == nil {
		return nil
	}

	switch v.Type() {
	case lua.LTNil:
		return nil
	case lua.LTBool:
		return lua.LVAsBool(v)
	case lua.LTNumber:
		return float64(v.(lua.LNumber))
	case lua.LTInteger:
		return int64(v.(lua.LInteger))
	case lua.LTString:
		return string(v.(lua.LString))
	case lua.LTTable:
		tbl := v.(*lua.LTable)
		maxn := tbl.MaxN()
		if maxn == 0 {
			return TableToMap(tbl)
		}
		return TableToSlice(tbl, maxn)
	case lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTChannel:
		fallthrough
	default:
		return v.String()
	}
}

// TableToMap converts a Lua table to a Go map.
func TableToMap(tbl *lua.LTable) map[string]any {
	result := make(map[string]any, tbl.Len())
	tbl.ForEach(func(key, value lua.LValue) {
		result[key.String()] = ToGoAny(value)
	})
	return result
}

// TableToSlice converts a Lua table to a Go slice.
func TableToSlice(tbl *lua.LTable, maxn int) []any {
	result := make([]any, 0, maxn)
	for i := 1; i <= maxn; i++ {
		result = append(result, ToGoAny(tbl.RawGetInt(i)))
	}
	return result
}
