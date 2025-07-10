package value

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// Global metatable storage using sync.Map for fast concurrent access
// All stored metatables are made immutable for safe reuse across goroutines
var metatableRegistry sync.Map

// Single LState used for creating shared function objects
var sharedState *lua.LState
var initOnce sync.Once

// getSharedState returns a singleton LState used for creating shared functions
func getSharedState() *lua.LState {
	initOnce.Do(func() {
		sharedState = lua.NewState()
	})
	return sharedState
}

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
// Functions are created once using a shared LState and the immutable metatables
// are safely reused across all LStates since the Go functions don't depend on environment.
// If a metatable already exists and is immutable, attempting to add new methods
// will create a new metatable (this allows for incremental registration).
func RegisterTypeMethods(
	_ *lua.LState, // Not used for function creation, only for validation
	typeName string,
	metamethods map[string]lua.LGFunction,
	methods map[string]lua.LGFunction,
) *lua.LTable {
	// Use shared state for function creation
	sharedL := getSharedState()

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
		mt = sharedL.CreateTable(0, totalSize)
	}

	// Add metamethods directly to metatable (only if not immutable)
	if !mt.Immutable {
		for name, fn := range metamethods {
			mt.RawSetString(name, sharedL.NewFunction(fn))
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
			indexTable = sharedL.CreateTable(0, len(methods))
			// Set __index to the methods table
			mt.RawSetString("__index", indexTable)
		}

		// Add all methods to indexTable using shared state
		for name, fn := range methods {
			indexTable.RawSetString(name, sharedL.NewFunction(fn))
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
