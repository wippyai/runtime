// Package engine provides utilities for converting between Lua and Go data types.
package engine

import (
	"github.com/ponyruntime/go-lua"
	"go.uber.org/zap"
)

// ToTable converts a Lua value to a Lua table if possible.
// Returns nil if the value cannot be converted to a table.
func ToTable(v lua.LValue) *lua.LTable {
	if lv, ok := v.(*lua.LTable); ok {
		return lv
	}
	return nil
}

// TableToSlice converts a Lua table to a slice of strings.
// Returns nil if the input is not a valid Lua table.
func TableToSlice(v lua.LValue, log *zap.Logger) []string {
	var ret []string

	if v.Type() != lua.LTTable {
		log.Warn("cannot parse table", zap.String("type", v.Type().String()))
		return nil
	}

	ToTable(v).ForEach(func(_, value lua.LValue) {
		ret = append(ret, value.String())
	})

	return ret
}

// TableToAnySlice converts a Lua table to a slice of interface{}.
// Returns nil if the input is not a valid Lua table.
func TableToAnySlice(v lua.LValue, log *zap.Logger) []any {
	var ret []any

	if v.Type() != lua.LTTable {
		log.Warn("cannot parse table", zap.String("type", v.Type().String()))
		return nil
	}

	ToTable(v).ForEach(func(_, value lua.LValue) {
		ret = append(ret, ToGoAny(value))
	})

	return ret
}

// TableToMap converts a Lua table to a map of strings.
// Returns nil if the input table is nil.
func TableToMap(t *lua.LTable, log *zap.Logger) map[string]string {
	if t == nil {
		log.Warn("table key exists, but the underlying table is nil")
		return nil
	}

	var ret = make(map[string]string)

	t.ForEach(func(key, val lua.LValue) {
		ret[key.String()] = val.String()
	})

	return ret
}

// GoToLua converts Go values to their Lua equivalents.
// Supports basic types, slices, and maps. Returns lua.LNil for unsupported types.
func GoToLua(l *lua.LState, v any) lua.LValue {
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
		table := l.NewTable()
		for i, v := range v {
			l.SetTable(table, lua.LNumber(i+1), lua.LNumber(v))
		}
		return table
	case []string:
		table := l.NewTable()
		for i, v := range v {
			l.SetTable(table, lua.LNumber(i+1), lua.LString(v))
		}
		return table
	case map[string]any:
		table := l.NewTable()
		for k, v := range v {
			l.SetTable(table, lua.LString(k), GoToLua(l, v))
		}
		return table
	case map[string]string:
		table := l.NewTable()
		for k, v := range v {
			l.SetTable(table, lua.LString(k), lua.LString(v))
		}
		return table
	case []any:
		table := l.NewTable()
		for i, v := range v {
			l.SetTable(table, lua.LNumber(i+1), GoToLua(l, v))
		}
		return table
	default:
		return lua.LNil
	}
}

// ToGoAny converts Lua values to their Go equivalents.
// Handles nil, boolean, number, string, and table types.
// Tables are converted to either maps or slices based on their structure.
func ToGoAny(v lua.LValue) any {
	switch v.Type() { //nolint:exhaustive
	case lua.LTNil:
		return v.String()
	case lua.LTBool:
		return lua.LVAsBool(v)
	case lua.LTNumber:
		return float64(v.(lua.LNumber))
	case lua.LTString:
		return string(v.(lua.LString))
	case lua.LTTable:
		tbl := v.(*lua.LTable)
		maxn := tbl.MaxN()
		if maxn == 0 { // Table is being used as a map
			result := make(map[string]any)
			tbl.ForEach(func(key, value lua.LValue) {
				result[key.String()] = ToGoAny(value)
			})
			return result
		} else { // Table is being used as an array
			result := make([]any, 0, maxn)
			for i := 1; i <= maxn; i++ {
				result = append(result, ToGoAny(tbl.RawGetInt(i)))
			}
			return result
		}
	default:
		return v.String()
	}
}
