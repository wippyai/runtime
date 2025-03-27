package builder

import (
	"database/sql"
	sqlmod "github.com/ponyruntime/pony/runtime/lua/modules/sql"
	"time"

	"github.com/Masterminds/squirrel"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// luaToGoValue converts a Lua value to a Go value
// Adds special handling for SQL-specific types
func luaToGoValue(l *lua.LState, value lua.LValue) interface{} {
	// Handle SQL-specific types first
	if ud, ok := value.(*lua.LUserData); ok {
		// Handle SQL NULL value
		if ud.Value == "SQL_NULL" {
			return nil
		}

		// Handle TypedValue from sql.as module
		if tv, ok := ud.Value.(*sqlmod.TypedValue); ok {
			return tv.Value
		}

		// Special handling for Sqlizer objects
		if _, ok := ud.Value.(squirrel.Sqlizer); ok {
			return ud.Value
		}

		// Handle time.Time
		if t, ok := ud.Value.(time.Time); ok {
			return t
		}
	}

	// Use the existing conversion function for standard types
	return luaconv.ToGoAny(value)
}

// goToLuaValue converts a Go value to a Lua value
func goToLuaValue(l *lua.LState, value interface{}) lua.LValue {
	// Handle SQL-specific types first
	switch v := value.(type) {
	case squirrel.Sqlizer:
		// Wrap Sqlizer in userdata with appropriate metatable
		ud := l.NewUserData()
		ud.Value = v
		ud.Metatable = getSqlizerMetatable(l)
		return ud

	case sql.NullString:
		if !v.Valid {
			return lua.LNil
		}
		return lua.LString(v.String)

	case sql.NullInt64:
		if !v.Valid {
			return lua.LNil
		}
		return lua.LNumber(v.Int64)

	case sql.NullFloat64:
		if !v.Valid {
			return lua.LNil
		}
		return lua.LNumber(v.Float64)

	case sql.NullBool:
		if !v.Valid {
			return lua.LNil
		}
		return lua.LBool(v.Bool)
	}

	// Use the existing conversion function for standard types
	result, err := luaconv.GoToLua(value)
	if err != nil {
		// Fall back to simple types if complex conversion fails
		return fallbackGoToLua(l, value)
	}
	return result
}

// fallbackGoToLua provides a simple fallback for GoToLua
func fallbackGoToLua(l *lua.LState, value interface{}) lua.LValue {
	if value == nil {
		return lua.LNil
	}

	switch v := value.(type) {
	case string:
		return lua.LString(v)
	case float64:
		return lua.LNumber(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case bool:
		return lua.LBool(v)
	default:
		// For unsupported types, create a basic userdata
		ud := l.NewUserData()
		ud.Value = v
		return ud
	}
}

// luaTableToMap converts a Lua table to a Go map[string]interface{}
// Used for WHERE conditions and other map-based operations
func luaTableToMap(l *lua.LState, table *lua.LTable) map[string]interface{} {
	result := make(map[string]interface{})

	table.ForEach(func(key, value lua.LValue) {
		// We only use string keys for maps
		if key.Type() == lua.LTString {
			result[key.String()] = luaToGoValue(l, value)
		}
	})

	return result
}

// luaTableToSlice converts a Lua array-like table to a Go []interface{}
func luaTableToSlice(l *lua.LState, table *lua.LTable) []interface{} {
	maxn := table.MaxN()
	result := make([]interface{}, maxn)

	for i := 1; i <= maxn; i++ {
		value := table.RawGetInt(i)
		result[i-1] = luaToGoValue(l, value)
	}

	return result
}

// luaTableToSqlizers converts a Lua table to a slice of Sqlizer
func luaTableToSqlizers(l *lua.LState, table *lua.LTable) ([]squirrel.Sqlizer, error) {
	result := make([]squirrel.Sqlizer, 0, table.Len())

	table.ForEach(func(_, value lua.LValue) {
		switch v := value.(type) {
		case *lua.LUserData:
			// Check if it's a Sqlizer
			if sqlizer, ok := v.Value.(squirrel.Sqlizer); ok {
				result = append(result, sqlizer)
			}
		case *lua.LTable:
			// Convert table to Eq
			eqMap := luaTableToMap(l, v)
			result = append(result, squirrel.Eq(eqMap))
		}
	})

	return result, nil
}

// isLuaArray determines if a Lua table is array-like
func isLuaArray(table *lua.LTable) bool {
	// Check if table.MaxN() > 0
	return table.MaxN() > 0
}
