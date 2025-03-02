package registry

import (
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/metamatch"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// luaTableToEntry converts a Lua table to a registry entry
func luaTableToEntry(l *lua.LState, table *lua.LTable) (regapi.Entry, error) {
	var entry regapi.Entry

	// Extract ID
	idVal := table.RawGetString("id")
	if idVal.Type() == lua.LTTable {
		idTable := idVal.(*lua.LTable)
		var err error
		entry.ID, err = tableToID(l, idTable)
		if err != nil {
			return entry, err
		}
	} else if idVal.Type() == lua.LTString {
		entry.ID = regapi.ParseID(idVal.String())
	} else {
		return entry, errors.New("entry must have valid id field")
	}

	// Extract kind
	kindVal := table.RawGetString("kind")
	if kindVal.Type() != lua.LTString {
		return entry, errors.New("entry must have kind field")
	}
	entry.Kind = regapi.Kind(kindVal.String())

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
		entry.Data = payload.NewPayload(dataVal, payload.Lua)
	}

	return entry, nil
}

// entryToLuaTable converts a registry Entry to a Lua table
func entryToLuaTable(l *lua.LState, entry regapi.Entry) (*lua.LTable, error) {
	// Create the base table
	entryTable := l.NewTable()

	// Convert ID
	entryTable.RawSetString("id", lua.LString(entry.ID.String()))

	// Add kind
	entryTable.RawSetString("kind", lua.LString(entry.Kind))

	// Convert metadata
	metaTable := l.NewTable()
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

// luaValueToGoValue converts a Lua value to an appropriate Go value for metadata
func luaValueToGoValue(v lua.LValue) (interface{}, bool) {
	switch v.Type() {
	case lua.LTString:
		return v.String(), true
	case lua.LTNumber:
		return float64(v.(lua.LNumber)), true
	case lua.LTBool:
		return bool(v.(lua.LBool)), true
	case lua.LTTable:
		// Handle table values as arrays
		array := make([]string, 0)
		v.(*lua.LTable).ForEach(func(_, item lua.LValue) {
			if item.Type() == lua.LTString {
				array = append(array, item.String())
			}
		})
		if len(array) > 0 {
			return array, true
		}
		return nil, false
	default:
		// skip unsupported types
		return nil, false
	}
}

// convertFilterToMetadata converts a Lua filter table to registry metadata
// for use with the finder interface
func convertFilterToMetadata(l *lua.LState, filterTable *lua.LTable) regapi.Metadata {
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
			if val, ok := luaValueToGoValue(v); ok {
				meta[key] = val
			}
		}
	})

	// Process nested metadata table
	metaVal := filterTable.RawGetString("meta")
	if metaVal.Type() == lua.LTTable {
		metaTable := metaVal.(*lua.LTable)
		metaTable.ForEach(func(k, v lua.LValue) {
			if kStr, ok := k.(lua.LString); ok {
				key := string(kStr)

				// Convert the Lua value to a Go value
				if val, ok := luaValueToGoValue(v); ok {
					meta[key] = val
				}
			}
		})
	}

	return meta
}

// metadataToMatcher converts registry metadata to a metamatch.Matcher
// leveraging the internal metamatch package for filtering
func metadataToMatcher(metadata regapi.Metadata) *metamatch.Matcher {
	matcher := metamatch.NewMatcher()

	// Add conditions for each metadata entry
	for key, value := range metadata {
		switch v := value.(type) {
		case string:
			matcher = matcher.WithStringValue(key, v)
		case bool:
			matcher = matcher.WithBoolValue(key, v)
		case int:
			matcher = matcher.WithIntValue(key, v)
		case float64:
			matcher = matcher.WithIntValue(key, int(v))
		case []string:
			// For string arrays, we assume all values must be present (AND logic)
			for _, tag := range v {
				matcher = matcher.WithTagContains(key, tag)
			}
		default:
			// For other types, use exact value matching
			matcher = matcher.WithExactValue(key, value)
		}
	}

	return matcher
}
