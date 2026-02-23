// SPDX-License-Identifier: MPL-2.0

package security

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

func luaTableToMetadata(_ *lua.LState, table *lua.LTable) attrs.Bag {
	meta := attrs.Bag{}
	table.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			meta[string(ks)] = value.ToGoAny(v)
		}
	})
	return meta
}

func optMetadataFromLuaTable(l *lua.LState, pos int) attrs.Bag {
	if metaTable := l.OptTable(pos, nil); metaTable != nil {
		return luaTableToMetadata(l, metaTable)
	}
	return attrs.Bag{}
}

func tableToMap(t *lua.LTable) map[string]any {
	result := make(map[string]any)
	t.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			result[string(ks)] = value.ToGoAny(v)
		}
	})
	return result
}
