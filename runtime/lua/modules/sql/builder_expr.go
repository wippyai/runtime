package sql

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

var sqlizerMethods = map[string]lua.LGoFunc{
	"to_sql": sqlizerToSQL,
}

var sqlizerMetamethods = map[string]lua.LGoFunc{
	"__tostring": sqlizerToString,
}

func wrapSqlizer(l *lua.LState, sqlizer squirrel.Sqlizer) {
	value.PushTypedUserData(l, sqlizer, sqlizerTypeName)
}

func sqlizerToString(l *lua.LState) int {
	ud := l.CheckUserData(1)
	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		l.Push(lua.LString("Invalid SQL Expression"))
		return 1
	}

	query, args, err := sqlizer.ToSql()
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("SQL Expression Error: %v", err)))
		return 1
	}

	l.Push(lua.LString(fmt.Sprintf("SQL: %s [Args: %v]", query, args)))
	return 1
}

func sqlizerToSQL(l *lua.LState) int {
	ud := l.CheckUserData(1)
	sqlizer, ok := ud.Value.(squirrel.Sqlizer)
	if !ok {
		l.ArgError(1, "expected Sqlizer")
		return 0
	}

	query, args, err := sqlizer.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, err.Error()).WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	l.Push(lua.LString(query))
	l.Push(goArgsToLuaTable(l, args))
	return 2
}

func builderExpr(l *lua.LState) int {
	if l.GetTop() < 1 || l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "expected string")
		return 0
	}

	sqlStr := l.CheckString(1)
	args := make([]any, l.GetTop()-1)

	for i := 2; i <= l.GetTop(); i++ {
		v := l.Get(i)
		if v.Type() == lua.LTUserData {
			if ud, ok := v.(*lua.LUserData); ok {
				if ud.Value == "SQL_NULL" {
					args[i-2] = nil
					continue
				}
				if typed, ok := ud.Value.(*TypedValue); ok {
					args[i-2] = typed.Value
					continue
				}
			}
		}

		if v == lua.LNil {
			args[i-2] = nil
		} else {
			args[i-2] = toGoValue(v)
		}
	}

	expr := squirrel.Expr(sqlStr, args...)
	wrapSqlizer(l, expr)
	return 1
}

func builderEq(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	eqMap := luaTableToMap(l.CheckTable(1))
	wrapSqlizer(l, squirrel.Eq(eqMap))
	return 1
}

func builderNotEq(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	notEqMap := luaTableToMap(l.CheckTable(1))
	wrapSqlizer(l, squirrel.NotEq(notEqMap))
	return 1
}

func builderLt(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	ltMap := luaTableToMap(l.CheckTable(1))
	wrapSqlizer(l, squirrel.Lt(ltMap))
	return 1
}

func builderLtOrEq(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	lteMap := luaTableToMap(l.CheckTable(1))
	wrapSqlizer(l, squirrel.LtOrEq(lteMap))
	return 1
}

func builderGt(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	gtMap := luaTableToMap(l.CheckTable(1))
	wrapSqlizer(l, squirrel.Gt(gtMap))
	return 1
}

func builderGtOrEq(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	gteMap := luaTableToMap(l.CheckTable(1))
	wrapSqlizer(l, squirrel.GtOrEq(gteMap))
	return 1
}

func builderLike(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	likeMap := luaTableToMap(l.CheckTable(1))
	wrapSqlizer(l, squirrel.Like(likeMap))
	return 1
}

func builderNotLike(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	notLikeMap := luaTableToMap(l.CheckTable(1))
	wrapSqlizer(l, squirrel.NotLike(notLikeMap))
	return 1
}

func builderAnd(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	table := l.CheckTable(1)
	parts := make([]squirrel.Sqlizer, 0, table.Len())

	table.ForEach(func(_, val lua.LValue) {
		switch v := val.(type) {
		case *lua.LUserData:
			if sqlizer, ok := v.Value.(squirrel.Sqlizer); ok {
				parts = append(parts, sqlizer)
			} else {
				l.RaiseError("expected Sqlizer in AND condition, got %T", v.Value)
			}
		case *lua.LTable:
			eqMap := luaTableToMap(v)
			parts = append(parts, squirrel.Eq(eqMap))
		default:
			l.RaiseError("expected Sqlizer or table in AND condition, got %s", val.Type().String())
		}
	})

	wrapSqlizer(l, squirrel.And(parts))
	return 1
}

func builderOr(l *lua.LState) int {
	if l.GetTop() != 1 || l.Get(1).Type() != lua.LTTable {
		l.ArgError(1, "expected table")
		return 0
	}

	table := l.CheckTable(1)
	parts := make([]squirrel.Sqlizer, 0, table.Len())

	table.ForEach(func(_, val lua.LValue) {
		switch v := val.(type) {
		case *lua.LUserData:
			if sqlizer, ok := v.Value.(squirrel.Sqlizer); ok {
				parts = append(parts, sqlizer)
			} else {
				l.RaiseError("expected Sqlizer in OR condition, got %T", v.Value)
			}
		case *lua.LTable:
			eqMap := luaTableToMap(v)
			parts = append(parts, squirrel.Eq(eqMap))
		default:
			l.RaiseError("expected Sqlizer or table in OR condition, got %s", val.Type().String())
		}
	})

	wrapSqlizer(l, squirrel.Or(parts))
	return 1
}
