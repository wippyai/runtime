package lua

import (
	"github.com/ponyruntime/pony/api/payload"
	lua "github.com/yuin/gopher-lua"
)

// ExportPayload exports a Lua value to a payload with optimized table handling.
// Tables are efficiently deep copied with proper capacity pre-allocation.
// Userdata objects are skipped and replaced with nil.
func ExportPayload(value lua.LValue) payload.Payload {
	processedValue := processLuaValue(value)
	return payload.NewPayload(processedValue, payload.Lua)
}

// processLuaValue handles different Lua value types, optimized for tables and userdata.
func processLuaValue(value lua.LValue) lua.LValue {
	switch value.Type() {
	case lua.LTTable:
		return deepCopyTable(value.(*lua.LTable))
	case lua.LTUserData:
		return lua.LNil // Skip userdata
	case lua.LTNil, lua.LTBool, lua.LTNumber, lua.LTString, lua.LTFunction, lua.LTThread, lua.LTChannel:
		// FIXME implement
		return value
	default:
		return value // Pass through other types unchanged
	}
}

// deepCopyTable creates an efficient deep copy of a Lua table, properly handling both
// array portions (sequential integer keys) and hash portions (non-sequential keys).
// This implementation is optimized based on the internal structure of LTable.
func deepCopyTable(table *lua.LTable) *lua.LTable {
	// Get the array length
	maxn := table.MaxN()

	// Create a new table with optimized capacity
	// Use defaultArrayCap (32) as minimum size for array part if needed
	arrayCap := maxn
	if arrayCap > 0 && arrayCap < 32 {
		arrayCap = 32
	}

	// No need to count hash keys - directly use the length of keys
	// This is much more efficient than iterating through all keys
	newTable := state.CreateTable(arrayCap, 0)

	// Copy array part (integer keys 1..maxn) if it exists
	if maxn > 0 {
		for i := 1; i <= maxn; i++ {
			val := table.RawGetInt(i)
			if val != lua.LNil {
				newTable.RawSetInt(i, processLuaValue(val))
			}
		}
	}

	// Copy string keys (most common case for hash part)
	table.ForEach(func(key, value lua.LValue) {
		// Skip array part keys which we already processed
		if key.Type() == lua.LTNumber {
			n := int(key.(lua.LNumber))
			if n > 0 && n <= maxn {
				return // Skip array elements
			}
		}

		// Process the value
		processedValue := processLuaValue(value)

		// Only add non-nil values
		if processedValue != lua.LNil {
			// Use the appropriate RawSet method based on key type for better performance
			switch k := key.(type) {
			case lua.LString:
				newTable.RawSetString(string(k), processedValue)
			case lua.LNumber:
				// Number but not an array index, or an out-of-range array index
				newTable.RawSetInt(int(k), processedValue)
			default:
				newTable.RawSet(key, processedValue)
			}
		}
	})

	return newTable
}
