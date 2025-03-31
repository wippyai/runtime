package security

import (
	"errors"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// luaTableToMetadata converts a Lua table to registry.Metadata
func luaTableToMetadata(l *lua.LState, table *lua.LTable) (registry.Metadata, error) {
	meta := registry.Metadata{}

	// Get transcoder from context
	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		return nil, errors.New("no transcoder found")
	}

	// Convert each key-value pair
	table.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			meta[string(ks)] = luaconv.ToGoAny(v)
		}
	})

	return meta, nil
}

// optMetadataFromLuaTable gets optional metadata from a Lua table
// Returns empty metadata if table is nil
func optMetadataFromLuaTable(l *lua.LState, pos int) (registry.Metadata, error) {
	if metaTable := l.OptTable(pos, nil); metaTable != nil {
		return luaTableToMetadata(l, metaTable)
	}
	return registry.Metadata{}, nil
}

// checkMetadataFromLuaTable gets required metadata from a Lua table
func checkMetadataFromLuaTable(l *lua.LState, pos int) (registry.Metadata, error) {
	metaTable := l.CheckTable(pos)
	return luaTableToMetadata(l, metaTable)
}
