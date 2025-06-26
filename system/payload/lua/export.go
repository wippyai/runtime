package lua

import (
	"github.com/ponyruntime/pony/api/payload"
	lua "github.com/yuin/gopher-lua"
)

// ExportPayload exports a Lua value to a payload with optimized table handling.
// Tables are made immutable in-place (including nested tables) and userdata is cleared.
// The original table structure is preserved but becomes immutable.
func ExportPayload(value lua.LValue) payload.Payload {
	processedValue := processAndImmutabilize(value)
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
