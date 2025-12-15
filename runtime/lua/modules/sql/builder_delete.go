package sql

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

type deleteBuilderWrapper struct {
	builder squirrel.DeleteBuilder
}

var deleteBuilderMethods = map[string]lua.LGoFunc{
	"from":               deleteFrom,
	"where":              deleteWhere,
	"order_by":           deleteOrderBy,
	"limit":              deleteLimit,
	"offset":             deleteOffset,
	"suffix":             deleteSuffix,
	"placeholder_format": deletePlaceholderFormat,
	"to_sql":             deleteToSQL,
	"run_with":           deleteRunWith,
}

var deleteBuilderMetamethods = map[string]lua.LGoFunc{
	"__tostring": deleteToString,
}

func builderDelete(l *lua.LState) int {
	var tableName string
	if l.GetTop() > 0 {
		tableName = l.CheckString(1)
	}

	wrapper := &deleteBuilderWrapper{
		builder: squirrel.Delete(tableName).PlaceholderFormat(squirrel.Question),
	}

	value.PushTypedUserData(l, wrapper, deleteBuilderTypeName)
	return 1
}

func checkDeleteBuilder(l *lua.LState) *deleteBuilderWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*deleteBuilderWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected DeleteBuilder object")
	return nil
}

func wrapDeleteBuilder(l *lua.LState, wrapper *deleteBuilderWrapper) {
	value.PushTypedUserData(l, wrapper, deleteBuilderTypeName)
}

func deleteToString(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("DeleteBuilder Error: %v", err)))
		return 1
	}

	l.Push(lua.LString(fmt.Sprintf("DeleteBuilder: %s [Args: %v]", query, args)))
	return 1
}

func deleteFrom(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)
	wrapDeleteBuilder(l, &deleteBuilderWrapper{
		builder: wrapper.builder.From(table),
	})
	return 1
}

func deleteWhere(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	var newBuilder squirrel.DeleteBuilder

	switch l.Get(2).Type() {
	case lua.LTString:
		condition := l.CheckString(2)
		args := make([]any, 0, l.GetTop()-2)
		for i := 3; i <= l.GetTop(); i++ {
			args = append(args, toGoValue(l.Get(i)))
		}
		newBuilder = wrapper.builder.Where(condition, args...)

	case lua.LTTable:
		table := l.CheckTable(2)
		eqMap := luaTableToMap(table)
		newBuilder = wrapper.builder.Where(squirrel.Eq(eqMap))

	case lua.LTUserData:
		ud := l.CheckUserData(2)
		if sqlizer, ok := ud.Value.(squirrel.Sqlizer); ok {
			newBuilder = wrapper.builder.Where(sqlizer)
		} else {
			l.ArgError(2, "expected string, table, or Sqlizer")
			return 0
		}

	default:
		l.ArgError(2, "expected string, table, or Sqlizer")
		return 0
	}

	wrapDeleteBuilder(l, &deleteBuilderWrapper{builder: newBuilder})
	return 1
}

func deleteOrderBy(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	orderBys := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		orderBys = append(orderBys, l.CheckString(i))
	}

	wrapDeleteBuilder(l, &deleteBuilderWrapper{
		builder: wrapper.builder.OrderBy(orderBys...),
	})
	return 1
}

func deleteLimit(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	limit := l.CheckInt64(2)
	if limit < 0 {
		limit = 0
	}

	wrapDeleteBuilder(l, &deleteBuilderWrapper{
		builder: wrapper.builder.Limit(uint64(limit)), //nolint:gosec // validated non-negative
	})
	return 1
}

func deleteOffset(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	offset := l.CheckInt64(2)
	if offset < 0 {
		offset = 0
	}

	wrapDeleteBuilder(l, &deleteBuilderWrapper{
		builder: wrapper.builder.Offset(uint64(offset)), //nolint:gosec // validated non-negative
	})
	return 1
}

func deleteSuffix(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	suffix := l.CheckString(2)
	args := make([]any, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, toGoValue(l.Get(i)))
	}

	wrapDeleteBuilder(l, &deleteBuilderWrapper{
		builder: wrapper.builder.Suffix(suffix, args...),
	})
	return 1
}

func deletePlaceholderFormat(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	format, ok := ud.Value.(squirrel.PlaceholderFormat)
	if !ok {
		l.ArgError(2, "expected placeholder format")
		return 0
	}

	wrapDeleteBuilder(l, &deleteBuilderWrapper{
		builder: wrapper.builder.PlaceholderFormat(format),
	})
	return 1
}

func deleteToSQL(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, err.Error()).WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	l.Push(lua.LString(query))
	l.Push(goArgsToLuaTable(l, args))
	return 2
}

func deleteRunWith(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	if db, ok := ud.Value.(*DB); ok {
		return newQueryExecutorFromDelete(l, db, wrapper.builder)
	}
	if tx, ok := ud.Value.(*Transaction); ok {
		return newQueryExecutorFromDeleteTx(l, tx, wrapper.builder)
	}
	l.ArgError(2, "database or transaction expected")
	return 0
}
