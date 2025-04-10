package builder

import (
	"fmt"
	"github.com/Masterminds/squirrel"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/modules/sql/sqlutil"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// Update the luaTableToMap function in expr.go to handle SQL_NULL special marker
func luaTableToMap(l *lua.LState, table *lua.LTable) map[string]interface{} {
	result := make(map[string]interface{})
	table.ForEach(func(key, value lua.LValue) {
		// Only use string keys
		if key.Type() == lua.LTString {
			keyStr := string(key.(lua.LString))

			// Check for SQL_NULL marker
			if value.Type() == lua.LTUserData {
				if ud, ok := value.(*lua.LUserData); ok {
					// Check if it's our NULL marker (from asNull function)
					if marker, ok := ud.Value.(string); ok && marker == "SQL_NULL" {
						result[keyStr] = nil
						return
					}

					// Handle typed values (from other as* functions)
					if typed, ok := ud.Value.(*sqlutil.TypedValue); ok {
						result[keyStr] = typed.Value
						return
					}
				}
			}

			// Use default conversion for non-NULL values
			result[keyStr] = luaconv.ToGoAny(value)
		}
	})
	return result
}

// registerExpressionBuilders adds expression builder functions to the module
func registerExpressionBuilders(l *lua.LState, mod *lua.LTable) {
	// Register the Sqlizer metatable first
	registerSqlizerMetatable(l)

	// Register expression builder functions
	mod.RawSetString("expr", l.NewFunction(builderExpr))
	mod.RawSetString("eq", l.NewFunction(builderEq))
	mod.RawSetString("not_eq", l.NewFunction(builderNotEq))
	mod.RawSetString("lt", l.NewFunction(builderLt))
	mod.RawSetString("lte", l.NewFunction(builderLtOrEq))
	mod.RawSetString("gt", l.NewFunction(builderGt))
	mod.RawSetString("gte", l.NewFunction(builderGtOrEq))
	mod.RawSetString("like", l.NewFunction(builderLike))
	mod.RawSetString("not_like", l.NewFunction(builderNotLike))
	mod.RawSetString("and_", l.NewFunction(builderAnd))
	mod.RawSetString("or_", l.NewFunction(builderOr))
}

// registerSqlizerMetatable registers the metatable for Sqlizer objects
func registerSqlizerMetatable(l *lua.LState) {
	// Define methods
	methods := map[string]lua.LGFunction{
		"to_sql": sqlizerToSql,
	}

	// Define metamethods
	metamethods := map[string]lua.LGFunction{
		"__tostring": sqlizerToString,
	}

	// Register metatable
	value.RegisterTypeMethods(l, "sql.Sqlizer", metamethods, methods)
}

// wrapSqlizer wraps a Sqlizer as a Lua userdata with the proper metatable
func wrapSqlizer(l *lua.LState, sqlizer squirrel.Sqlizer) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = sqlizer
	ud.Metatable = value.GetTypeMetatable(l, "sql.Sqlizer")
	return ud
}

// sqlizerToString is the __tostring metamethod for Sqlizer objects
func sqlizerToString(l *lua.LState) int {
	ud := l.CheckUserData(1)
	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		l.Push(lua.LString("Invalid SQL Expression"))
		return 1
	}

	// Get the SQL and args
	query, args, err := sqlizer.ToSql()
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("SQL Expression Error: %v", err)))
		return 1
	}

	// Format the result with parameters for debugging (similar to DebugSqlizer)
	result := fmt.Sprintf("SQL: %s [Args: %v]", query, args)
	l.Push(lua.LString(result))
	return 1
}

// sqlizerToSql implements the to_sql method for Sqlizer objects
func sqlizerToSql(l *lua.LState) int {
	ud := l.CheckUserData(1)
	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		l.ArgError(1, "expected Sqlizer")
		return 0
	}

	query, args, err := sqlizer.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(query))

	// Convert args to Lua values
	argsTable := l.CreateTable(len(args), 0)
	for i, arg := range args {
		luaValue, err := luaconv.GoToLua(arg)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("conversion error: %v", err)))
			return 2
		}
		argsTable.RawSetInt(i+1, luaValue)
	}
	l.Push(argsTable)

	return 2
}

// builderExpr creates raw SQL expressions with placeholders
// Usage: sql.builder.expr("COALESCE(?, ?)", nil, "default")
func builderExpr(l *lua.LState) int {
	// Check for at least a SQL string
	if l.GetTop() < 1 || l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "expected string")
		return 0
	}

	sql := l.CheckString(1)

	// Collect arguments
	args := make([]interface{}, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		v := l.Get(i)
		// Check for special NULL value
		if v.Type() == lua.LTUserData {
			if ud, ok := v.(*lua.LUserData); ok {
				// Check for SQL NULL
				if ud.Value == "SQL_NULL" {
					args[i-2] = nil
					continue
				}

				// Check for typed values
				if typedValue, ok := ud.Value.(*sqlutil.TypedValue); ok {
					// Use the pre-converted value with the right type
					args[i-2] = typedValue.Value
					continue
				}
			}
		}

		// Handle normal values
		if v == lua.LNil {
			args[i-2] = nil
		} else {
			// Convert using existing mechanism for a single value
			args[i-2] = luaconv.ToGoAny(v)
		}
	} // todo: more tests please!

	// Create Expr and wrap it
	expr := squirrel.Expr(sql, args...)
	ud := wrapSqlizer(l, expr)

	l.Push(ud)
	return 1
}

// builderEq creates an equality condition (field = value)
// Usage: sql.builder.eq({id = 1, name = "test"})
func builderEq(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	// Convert Lua table to Go map
	eqMap := luaTableToMap(l, l.CheckTable(1))

	// Create Eq expression and wrap it
	eq := squirrel.Eq(eqMap)
	ud := wrapSqlizer(l, eq)

	l.Push(ud)
	return 1
}

// builderNotEq creates an inequality condition (field <> value)
// Usage: sql.builder.not_eq({id = 1, name = "test"})
func builderNotEq(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	// Convert Lua table to Go map
	eqMap := luaTableToMap(l, l.CheckTable(1))

	// Create NotEq expression and wrap it
	notEq := squirrel.NotEq(eqMap)
	ud := wrapSqlizer(l, notEq)

	l.Push(ud)
	return 1
}

// builderLt creates a less than condition (field < value)
// Usage: sql.builder.lt({id = 1000, created_at = timestamp})
func builderLt(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	// Convert Lua table to Go map
	ltMap := luaTableToMap(l, l.CheckTable(1))

	// Create Lt expression and wrap it
	lt := squirrel.Lt(ltMap)
	ud := wrapSqlizer(l, lt)

	l.Push(ud)
	return 1
}

// builderLtOrEq creates a less than or equal condition (field <= value)
// Usage: sql.builder.lte({id = 1000, created_at = timestamp})
func builderLtOrEq(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	// Convert Lua table to Go map
	lteMap := luaTableToMap(l, l.CheckTable(1))

	// Create LtOrEq expression and wrap it
	ltOrEq := squirrel.LtOrEq(lteMap)
	ud := wrapSqlizer(l, ltOrEq)

	l.Push(ud)
	return 1
}

// builderGt creates a greater than condition (field > value)
// Usage: sql.builder.gt({id = 1000, created_at = timestamp})
func builderGt(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	// Convert Lua table to Go map
	gtMap := luaTableToMap(l, l.CheckTable(1))

	// Create Gt expression and wrap it
	gt := squirrel.Gt(gtMap)
	ud := wrapSqlizer(l, gt)

	l.Push(ud)
	return 1
}

// builderGtOrEq creates a greater than or equal condition (field >= value)
// Usage: sql.builder.gte({id = 1000, created_at = timestamp})
func builderGtOrEq(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	// Convert Lua table to Go map
	gteMap := luaTableToMap(l, l.CheckTable(1))

	// Create GtOrEq expression and wrap it
	gtOrEq := squirrel.GtOrEq(gteMap)
	ud := wrapSqlizer(l, gtOrEq)

	l.Push(ud)
	return 1
}

// builderLike creates a LIKE condition (field LIKE pattern)
// Usage: sql.builder.like({name = "test%"})
func builderLike(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	// Convert Lua table to Go map
	likeMap := luaTableToMap(l, l.CheckTable(1))

	// Create Like expression and wrap it
	like := squirrel.Like(likeMap)
	ud := wrapSqlizer(l, like)

	l.Push(ud)
	return 1
}

// builderNotLike creates a NOT LIKE condition (field NOT LIKE pattern)
// Usage: sql.builder.not_like({name = "test%"})
func builderNotLike(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	// Convert Lua table to Go map
	notLikeMap := luaTableToMap(l, l.CheckTable(1))

	// Create NotLike expression and wrap it
	notLike := squirrel.NotLike(notLikeMap)
	ud := wrapSqlizer(l, notLike)

	l.Push(ud)
	return 1
}

// builderAnd creates an AND condition combining multiple conditions
// Usage: sql.builder.and({condition1, condition2, ...})
func builderAnd(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	table := l.CheckTable(1)
	parts := make([]squirrel.Sqlizer, 0, table.Len())

	// Convert each table element to Sqlizer
	table.ForEach(func(_, value lua.LValue) {
		switch v := value.(type) {
		case *lua.LUserData:
			if sqlizer, ok := v.Value.(squirrel.Sqlizer); ok {
				parts = append(parts, sqlizer)
			} else {
				l.RaiseError("expected Sqlizer in AND condition, got %T", v.Value)
			}
		case *lua.LTable:
			// Convert tables to Eq by default
			eqMap := luaTableToMap(l, v)
			parts = append(parts, squirrel.Eq(eqMap))
		default:
			l.RaiseError("expected Sqlizer or table in AND condition, got %s", value.Type().String())
		}
	})

	// Create And expression and wrap it
	and := squirrel.And(parts)
	ud := wrapSqlizer(l, and)

	l.Push(ud)
	return 1
}

// builderOr creates an OR condition combining multiple conditions
// Usage: sql.builder.or({condition1, condition2, ...})
func builderOr(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	table := l.CheckTable(1)
	parts := make([]squirrel.Sqlizer, 0, table.Len())

	// Convert each table element to Sqlizer
	table.ForEach(func(_, value lua.LValue) {
		switch v := value.(type) {
		case *lua.LUserData:
			if sqlizer, ok := v.Value.(squirrel.Sqlizer); ok {
				parts = append(parts, sqlizer)
			} else {
				l.RaiseError("expected Sqlizer in OR condition, got %T", v.Value)
			}
		case *lua.LTable:
			// Convert tables to Eq by default
			eqMap := luaTableToMap(l, v)
			parts = append(parts, squirrel.Eq(eqMap))
		default:
			l.RaiseError("expected Sqlizer or table in OR condition, got %s", value.Type().String())
		}
	})

	// Create Or expression and wrap it
	or := squirrel.Or(parts)
	ud := wrapSqlizer(l, or)

	l.Push(ud)
	return 1
}
