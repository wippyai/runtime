package security

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
)

func luaTableToMetadata(_ *lua.LState, table *lua.LTable) attrs.Bag {
	meta := attrs.Bag{}
	table.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			meta[string(ks)] = toGoValue(v)
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

func toLuaValue(l *lua.LState, val any) lua.LValue {
	if val == nil {
		return lua.LNil
	}
	switch v := val.(type) {
	case string:
		return lua.LString(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case float64:
		return lua.LNumber(v)
	case bool:
		return lua.LBool(v)
	case []byte:
		return lua.LString(v)
	case map[string]any:
		t := l.CreateTable(0, len(v))
		for k, val := range v {
			t.RawSetString(k, toLuaValue(l, val))
		}
		return t
	case []any:
		t := l.CreateTable(len(v), 0)
		for i, val := range v {
			t.RawSetInt(i+1, toLuaValue(l, val))
		}
		return t
	default:
		return lua.LNil
	}
}

func toGoValue(lv lua.LValue) any {
	switch v := lv.(type) {
	case lua.LString:
		return string(v)
	case lua.LNumber:
		return float64(v)
	case lua.LInteger:
		return int64(v)
	case lua.LBool:
		return bool(v)
	case *lua.LNilType:
		return nil
	case *lua.LTable:
		return tableToMap(v)
	default:
		return nil
	}
}

func tableToMap(t *lua.LTable) map[string]any {
	result := make(map[string]any)
	t.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			result[string(ks)] = toGoValue(v)
		}
	})
	return result
}
