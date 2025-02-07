package lua

import (
	lua "github.com/yuin/gopher-lua"
)

// Helper functions for Lua transcoding
var lState *lua.LState = lua.NewState()

// ToGoAny converts a lua.LValue to its Go equivalent.
func ToGoAny(v lua.LValue) any {
	switch v.Type() { //nolint:exhaustive
	case lua.LTNil:
		return nil
	case lua.LTBool:
		return lua.LVAsBool(v)
	case lua.LTNumber:
		return float64(v.(lua.LNumber))
	case lua.LTString:
		return string(v.(lua.LString))
	case lua.LTTable:
		tbl := v.(*lua.LTable)
		maxn := tbl.MaxN()
		if maxn == 0 {
			return tableToMap(tbl)
		}
		return tableToSlice(tbl, maxn)
	default:
		return v.String()
	}
}

func tableToMap(tbl *lua.LTable) map[string]any {
	result := make(map[string]any)
	tbl.ForEach(func(key, value lua.LValue) {
		result[key.String()] = ToGoAny(value)
	})
	return result
}

func tableToSlice(tbl *lua.LTable, maxn int) []any {
	result := make([]any, 0, maxn)
	for i := 1; i <= maxn; i++ {
		result = append(result, ToGoAny(tbl.RawGetInt(i)))
	}
	return result
}

// GoToLua converts a Go value to its Lua equivalent.
func GoToLua(v any) lua.LValue {
	// todo: handle errors

	switch v := v.(type) {
	case string:
		return lua.LString(v)
	case float64:
		return lua.LNumber(v)
	case int:
		return lua.LNumber(v)
	case bool:
		return lua.LBool(v)
	case nil:
		return lua.LNil
	case []int:
		table := lState.NewTable()
		for i, v := range v {
			lState.SetTable(table, lua.LNumber(i+1), lua.LNumber(v))
		}
		return table
	case []string:
		table := lState.NewTable()
		for i, v := range v {
			lState.SetTable(table, lua.LNumber(i+1), lua.LString(v))
		}
		return table
	case map[string]any:
		table := lState.NewTable()
		for k, v := range v {
			lState.SetTable(table, lua.LString(k), GoToLua(v))
		}
		return table
	case map[string]string:
		table := lState.NewTable()
		for k, v := range v {
			lState.SetTable(table, lua.LString(k), lua.LString(v))
		}
		return table
	case []any:
		table := lState.NewTable()
		for i, v := range v {
			lState.SetTable(table, lua.LNumber(i+1), GoToLua(v))
		}
		return table
	default:
		return lua.LNil // Fallback for unsupported types
	}
}
