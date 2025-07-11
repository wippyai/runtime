package registry

import (
	"errors"
	"fmt"

	"github.com/ponyruntime/pony/api/payload"
	regapi "github.com/ponyruntime/pony/api/registry"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// luaTableToEntry converts a Lua table to a registry entry
func luaTableToEntry(l *lua.LState, table *lua.LTable) (regapi.Entry, error) {
	var entry regapi.Entry

	// Extract id
	idVal := table.RawGetString("id")
	switch idVal.Type() {
	case lua.LTTable:
		idTable := idVal.(*lua.LTable)
		var err error
		entry.ID, err = tableToID(l, idTable)
		if err != nil {
			return entry, err
		}
	case lua.LTString:
		entry.ID = regapi.ParseID(idVal.String())
	case lua.LTNil, lua.LTBool, lua.LTNumber, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTChannel:
		// FIXME rework on demand
		fallthrough
	default:
		return entry, errors.New("entry must have valid id field")
	}

	// Extract kind
	kindVal := table.RawGetString("kind")
	if kindVal.Type() != lua.LTString {
		return entry, errors.New("entry must have kind field")
	}
	entry.Kind = kindVal.String()

	// Extract metadata
	metaVal := table.RawGetString("meta")
	if metaVal.Type() == lua.LTTable {
		meta := regapi.Metadata{}
		metaTable := metaVal.(*lua.LTable)

		metaTable.ForEach(func(k, v lua.LValue) {
			if kStr, ok := k.(lua.LString); ok {
				meta[string(kStr)] = luaconv.ToGoAny(v)
			}
		})

		entry.Meta = meta
	} else {
		entry.Meta = regapi.Metadata{}
	}

	// Extract data
	dataVal := table.RawGetString("data")
	if dataVal != lua.LNil {
		// Convert to payload
		entry.Data = luaconv.ExportPayload(dataVal)
	}

	return entry, nil
}

// entryToLuaTable converts a registry Entry to a Lua table
func entryToLuaTable(l *lua.LState, entry regapi.Entry) (*lua.LTable, error) {
	// Create the base table
	entryTable := l.CreateTable(0, 4)

	// Convert id
	entryTable.RawSetString("id", lua.LString(entry.ID.String()))

	// Add kind
	entryTable.RawSetString("kind", lua.LString(entry.Kind))

	// Convert metadata
	metaTable := l.CreateTable(0, len(entry.Meta))
	for k, v := range entry.Meta {
		luaValue, err := luaconv.GoToLua(v)
		if err != nil {
			return nil, fmt.Errorf("failed to convert metadata value: %w", err)
		}
		metaTable.RawSetString(k, luaValue)
	}
	entryTable.RawSetString("meta", metaTable)

	// Convert data payload using transcoder if available
	if entry.Data != nil {
		dtt := payload.GetTranscoder(l.Context())
		if dtt != nil {
			luaData, err := dtt.Transcode(entry.Data, payload.Lua)
			if err != nil {
				return nil, fmt.Errorf("failed to transcode entry data: %w", err)
			}
			entryTable.RawSetString("data", luaData.Data().(lua.LValue))
		} else {
			return nil, fmt.Errorf("failed to transcode entry data: no transcoder")
		}
	} else {
		entryTable.RawSetString("data", lua.LNil)
	}

	return entryTable, nil
}

// convertFilterToMetadata converts a Lua filter table to registry metadata
// for use with the finder interface
func convertFilterToMetadata(_ *lua.LState, filterTable *lua.LTable) regapi.Metadata {
	meta := regapi.Metadata{}

	// Process top-level filter properties directly
	filterTable.ForEach(func(k, v lua.LValue) {
		if kStr, ok := k.(lua.LString); ok {
			key := string(kStr)

			// Skip "meta" key as it's handled separately
			if key == "meta" {
				return
			}

			// Convert the Lua value to a Go value
			meta[key] = luaconv.ToGoAny(v)
		}
	})

	// Process nested metadata table
	metaVal := filterTable.RawGetString("meta")
	if metaVal.Type() == lua.LTTable {
		metaTable := metaVal.(*lua.LTable)
		metaTable.ForEach(func(k, v lua.LValue) {
			if kStr, ok := k.(lua.LString); ok {
				key := string(kStr)

				meta[key] = luaconv.ToGoAny(v)
			}
		})
	}

	return meta
}
