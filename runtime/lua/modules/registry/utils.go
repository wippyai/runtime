package registry

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// luaTableToEntry converts a Lua table to a registry entry
func luaTableToEntry(l *lua.LState, table *lua.LTable) (regapi.Entry, error) {
	var entry regapi.Entry

	// Extract ID
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
	case lua.LTNil, lua.LTBool, lua.LTNumber, lua.LTInteger, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTChannel:
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
		meta := attrs.Bag{}
		metaTable := metaVal.(*lua.LTable)

		metaTable.ForEach(func(k, v lua.LValue) {
			if kStr, ok := k.(lua.LString); ok {
				meta[string(kStr)] = value.ToGoAny(v)
			}
		})

		entry.Meta = meta
	} else {
		entry.Meta = attrs.Bag{}
	}

	// Extract data
	dataVal := table.RawGetString("data")
	if dataVal != lua.LNil {
		entry.Data = payload.NewPayload(value.ToGoAny(dataVal), payload.Golang)
	}

	return entry, nil
}

// entryToLuaTable converts a registry Entry to a Lua table
func entryToLuaTable(l *lua.LState, entry regapi.Entry) (*lua.LTable, error) {
	entryTable := l.CreateTable(0, 4)

	// Convert ID
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
func convertFilterToMetadata(_ *lua.LState, filterTable *lua.LTable) attrs.Bag {
	meta := attrs.Bag{}

	filterTable.ForEach(func(k, v lua.LValue) {
		if kStr, ok := k.(lua.LString); ok {
			key := string(kStr)

			if key == "meta" {
				return
			}

			meta[key] = value.ToGoAny(v)
		}
	})

	// Process nested metadata table
	metaVal := filterTable.RawGetString("meta")
	if metaVal.Type() == lua.LTTable {
		metaTable := metaVal.(*lua.LTable)
		metaTable.ForEach(func(k, v lua.LValue) {
			if kStr, ok := k.(lua.LString); ok {
				key := string(kStr)
				meta[key] = value.ToGoAny(v)
			}
		})
	}

	return meta
}
