package payload

import (
	"sync"

	"github.com/wippyai/runtime/api/payload"
	lua "github.com/yuin/gopher-lua"
)

const (
	UseDeepCopy = true // Set to true for deep copy, false for immutable in-place
)

// --- POOLING IMPLEMENTATION ---

// visitedMapPool holds maps used to track visited tables during recursive operations
// to prevent infinite loops and reduce GC pressure in hot paths.
var visitedMapPool = sync.Pool{
	New: func() any {
		// The pool creates a new map when it's empty.
		// We initialize with a small capacity; it will grow as needed.
		return make(map[*lua.LTable]*lua.LTable)
	},
}

// immutableVisitedMapPool is a separate pool for the immutable function,
// as the map value type is different (bool vs *lua.LTable).
var immutableVisitedMapPool = sync.Pool{
	New: func() any {
		return make(map[*lua.LTable]bool)
	},
}

// --- END POOLING IMPLEMENTATION ---

// ExportPayload exports a Lua value to a payload with optimized table handling.
func ExportPayload(value lua.LValue) payload.Payload {
	var processedValue lua.LValue
	if UseDeepCopy {
		processedValue = processAndDeepCopy(value)
	} else {
		processedValue = processAndImmutabilize(value)
	}
	return payload.NewPayload(processedValue, payload.Lua)
}

// processAndImmutabilize processes a Lua value, making tables immutable in-place.
func processAndImmutabilize(value lua.LValue) lua.LValue {
	// Get a map from the pool.
	visited := immutableVisitedMapPool.Get().(map[*lua.LTable]bool)
	defer func() {
		// This ensures the next user gets a clean map.
		clear(visited)
		immutableVisitedMapPool.Put(visited)
	}()

	return makeTableImmutableRecursive(value, visited)
}

// processAndDeepCopy processes a Lua value, deep copying tables and clearing userdata.
func processAndDeepCopy(value lua.LValue) lua.LValue {
	// Get a map from the pool instead of allocating a new one every time.
	visited := visitedMapPool.Get().(map[*lua.LTable]*lua.LTable)
	defer func() {
		clear(visited)
		visitedMapPool.Put(visited)
	}()

	return copyValue(value, visited)
}

// makeTableImmutableRecursive makes a table and all nested tables immutable.
func makeTableImmutableRecursive(value lua.LValue, visited map[*lua.LTable]bool) lua.LValue {
	table, ok := value.(*lua.LTable)
	if !ok {
		if _, isLuaError := value.(*lua.Error); isLuaError {
			return value
		}
		if ud, isUserdata := value.(*lua.LUserData); isUserdata {
			if ud.Value == nil {
				return lua.LNil
			}
			if err, ok := ud.Value.(error); ok {
				return lua.LString(err.Error())
			}
			return lua.LNil
		}
		return value
	}

	if visited[table] {
		return table
	}
	visited[table] = true

	if table.Array != nil {
		for i, v := range table.Array {
			table.Array[i] = makeTableImmutableRecursive(v, visited)
		}
	}
	if table.Strdict != nil {
		for key, v := range table.Strdict {
			table.Strdict[key] = makeTableImmutableRecursive(v, visited)
		}
	}
	if table.Dict != nil {
		for key, v := range table.Dict {
			table.Dict[key] = makeTableImmutableRecursive(v, visited)
		}
	}

	table.Immutable = true
	return table
}

// deepCopyTable creates a deep copy of a table, recursively copying nested tables.
func deepCopyTable(original *lua.LTable, visited map[*lua.LTable]*lua.LTable) *lua.LTable {
	if newTable, ok := visited[original]; ok {
		return newTable
	}

	newTable := &lua.LTable{Metatable: lua.LNil, Immutable: false}
	visited[original] = newTable

	if original.Metatable != lua.LNil {
		if mt, ok := original.Metatable.(*lua.LTable); ok {
			newTable.Metatable = deepCopyTable(mt, visited)
		} else {
			newTable.Metatable = copyValue(original.Metatable, visited)
		}
	}
	if original.Array != nil {
		newTable.Array = make([]lua.LValue, len(original.Array))
		for i, v := range original.Array {
			newTable.Array[i] = copyValue(v, visited)
		}
	}
	if original.Strdict != nil {
		newTable.Strdict = make(map[string]lua.LValue, len(original.Strdict))
		for key, v := range original.Strdict {
			newTable.Strdict[key] = copyValue(v, visited)
		}
	}
	if original.Dict != nil {
		newTable.Dict = make(map[lua.LValue]lua.LValue, len(original.Dict))
		for key, v := range original.Dict {
			newKey := copyValue(key, visited)
			newValue := copyValue(v, visited)
			newTable.Dict[newKey] = newValue
		}
	}
	if original.Keys != nil {
		newTable.Keys = make([]lua.LValue, len(original.Keys))
		newTable.K2i = make(map[lua.LValue]int, len(original.K2i))
		for i, key := range original.Keys {
			newKey := copyValue(key, visited)
			newTable.Keys[i] = newKey
			newTable.K2i[newKey] = i
		}
	}

	return newTable
}

// copyValue recursively copies a value, passing the visited map down.
func copyValue(value lua.LValue, visited map[*lua.LTable]*lua.LTable) lua.LValue {
	switch v := value.(type) {
	case *lua.LTable:
		return deepCopyTable(v, visited)
	case *lua.Error:
		return v
	case *lua.LUserData:
		if v.Value == nil {
			return lua.LNil
		}
		if err, ok := v.Value.(error); ok {
			return lua.LString(err.Error())
		}
		return lua.LNil
	default:
		return value
	}
}
