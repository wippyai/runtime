package lua

import (
	"github.com/yuin/gopher-lua"
)

// Helper functions for Lua transcoding

// ToGoAny converts a lua.LValue to its Go equivalent.
func ToGoAny(v lua.LValue) any {
	switch v.Type() { //nolint:exhaustive
	case lua.LTNil:
		return nil // Use nil instead of v.String() for nil values
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
		return v.String() // Fallback for other types
	}
}

// GoToLua converts a Go value to its Lua equivalent.
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
		return lua.LNil // Fallback for unsupported types
	}
}
