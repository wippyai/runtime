package lua

import (
	"github.com/ponyruntime/pony/api/payload"
	lua "github.com/yuin/gopher-lua"
)

const (
	UseDeepCopy = true // Set to true for deep copy, false for immutable in-place
)

// ExportPayload exports a Lua value to a payload with optimized table handling.
// Behavior depends on UseDeepCopy constant.
func ExportPayload(value lua.LValue) payload.Payload {
	var processedValue lua.LValue
	if UseDeepCopy {
		processedValue = processAndDeepCopy(value)
	} else {
		processedValue = processAndImmutabilize(value)
	}
	return payload.NewPayload(processedValue, payload.Lua)
}

// processAndImmutabilize processes a Lua value, making tables immutable and clearing userdata.
func processAndImmutabilize(value lua.LValue) lua.LValue {
	switch v := value.(type) {
	case *lua.LTable:
		return makeTableImmutableRecursive(v)
	case *lua.LUserData:
		return lua.LNil // Clear userdata
	default:
		return value // Pass through other types unchanged
	}
}

// processAndDeepCopy processes a Lua value, deep copying tables and clearing userdata.
func processAndDeepCopy(value lua.LValue) lua.LValue {
	switch v := value.(type) {
	case *lua.LTable:
		return deepCopyTable(v)
	case *lua.LUserData:
		return lua.LNil
	default:
		return value
	}
}

// makeTableImmutableRecursive makes a table and all nested tables immutable,
// and clears any userdata found in the process. This is done in-place for efficiency.
func makeTableImmutableRecursive(table *lua.LTable) *lua.LTable {
	// Process array part directly
	if table.Array != nil {
		for i, value := range table.Array {
			switch v := value.(type) {
			case *lua.LTable:
				makeTableImmutableRecursive(v)
			case *lua.LUserData:
				table.Array[i] = lua.LNil
			}
		}
	}

	// Process string dictionary part directly
	if table.Strdict != nil {
		for key, value := range table.Strdict {
			switch v := value.(type) {
			case *lua.LTable:
				makeTableImmutableRecursive(v)
			case *lua.LUserData:
				table.Strdict[key] = lua.LNil
			}
		}
	}

	// Process general dictionary part directly
	if table.Dict != nil {
		for key, value := range table.Dict {
			switch v := value.(type) {
			case *lua.LTable:
				makeTableImmutableRecursive(v)
			case *lua.LUserData:
				table.Dict[key] = lua.LNil
			}
		}
	}

	// Make this table immutable
	table.Immutable = true
	return table
}

// deepCopyTable creates a deep copy of a table, recursively copying nested tables and clearing userdata.
func deepCopyTable(original *lua.LTable) *lua.LTable {
	newTable := &lua.LTable{
		Metatable: lua.LNil,
		Immutable: false,
	}

	// Copy metatable if it's not nil and not userdata
	if original.Metatable != lua.LNil {
		switch mt := original.Metatable.(type) {
		case *lua.LTable:
			newTable.Metatable = deepCopyTable(mt)
		case *lua.LUserData:
			newTable.Metatable = lua.LNil
		default:
			newTable.Metatable = original.Metatable
		}
	}

	// Copy array part
	if original.Array != nil {
		newTable.Array = make([]lua.LValue, len(original.Array))
		for i, value := range original.Array {
			newTable.Array[i] = copyValue(value)
		}
	}

	// Copy string dictionary part
	if original.Strdict != nil {
		newTable.Strdict = make(map[string]lua.LValue, len(original.Strdict))
		for key, value := range original.Strdict {
			newTable.Strdict[key] = copyValue(value)
		}
	}

	// Copy general dictionary part
	if original.Dict != nil {
		newTable.Dict = make(map[lua.LValue]lua.LValue, len(original.Dict))
		for key, value := range original.Dict {
			newKey := copyValue(key)
			newValue := copyValue(value)
			newTable.Dict[newKey] = newValue
		}
	}

	// Copy Keys and K2i if they exist
	if original.Keys != nil {
		newTable.Keys = make([]lua.LValue, len(original.Keys))
		newTable.K2i = make(map[lua.LValue]int, len(original.K2i))
		for i, key := range original.Keys {
			newKey := copyValue(key)
			newTable.Keys[i] = newKey
			newTable.K2i[newKey] = i
		}
	}

	return newTable
}

// copyValue recursively copies a value, handling nested tables and clearing userdata.
func copyValue(value lua.LValue) lua.LValue {
	switch v := value.(type) {
	case *lua.LTable:
		return deepCopyTable(v)
	case *lua.LUserData:
		return lua.LNil
	default:
		return value // primitives are copied by value
	}
}
