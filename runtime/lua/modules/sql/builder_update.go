package sql

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

type updateBuilderWrapper struct {
	builder squirrel.UpdateBuilder
}

var updateBuilderMethods = map[string]lua.LGoFunc{
	"table":              updateTable,
	"set":                updateSet,
	"set_map":            updateSetMap,
	"where":              updateWhere,
	"order_by":           updateOrderBy,
	"limit":              updateLimit,
	"offset":             updateOffset,
	"suffix":             updateSuffix,
	"from":               updateFrom,
	"from_select":        updateFromSelect,
	"placeholder_format": updatePlaceholderFormat,
	"to_sql":             updateToSQL,
	"run_with":           updateRunWith,
}

var updateBuilderMetamethods = map[string]lua.LGoFunc{
	"__tostring": updateToString,
}

func builderUpdate(l *lua.LState) int {
	var tableName string
	if l.GetTop() > 0 {
		tableName = l.CheckString(1)
	}

	wrapper := &updateBuilderWrapper{
		builder: squirrel.Update(tableName).PlaceholderFormat(squirrel.Question),
	}

	value.PushTypedUserData(l, wrapper, updateBuilderTypeName)
	return 1
}

func checkUpdateBuilder(l *lua.LState) *updateBuilderWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*updateBuilderWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected UpdateBuilder object")
	return nil
}

func wrapUpdateBuilder(l *lua.LState, wrapper *updateBuilderWrapper) {
	value.PushTypedUserData(l, wrapper, updateBuilderTypeName)
}

func updateToString(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("UpdateBuilder Error: %v", err)))
		return 1
	}

	l.Push(lua.LString(fmt.Sprintf("UpdateBuilder: %s [Args: %v]", query, args)))
	return 1
}

func updateTable(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)
	wrapUpdateBuilder(l, &updateBuilderWrapper{
		builder: wrapper.builder.Table(table),
	})
	return 1
}

func updateSet(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	column := l.CheckString(2)
	valueParam := l.Get(3)

	var v any
	switch valueParam.Type() { //nolint:exhaustive // only userdata needs special handling
	case lua.LTUserData:
		if ud, ok := valueParam.(*lua.LUserData); ok {
			if sqlizer, ok := ud.Value.(squirrel.Sqlizer); ok {
				v = sqlizer
			} else if ud.Value == "SQL_NULL" {
				v = nil
			} else if typed, ok := ud.Value.(*TypedValue); ok {
				v = typed.Value
			} else {
				l.ArgError(3, "invalid userdata")
				return 0
			}
		}
	default:
		v = toGoValue(valueParam)
	}

	wrapUpdateBuilder(l, &updateBuilderWrapper{
		builder: wrapper.builder.Set(column, v),
	})
	return 1
}

func updateSetMap(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckTable(2)
	valuesMap := luaTableToMap(table)

	wrapUpdateBuilder(l, &updateBuilderWrapper{
		builder: wrapper.builder.SetMap(valuesMap),
	})
	return 1
}

func updateWhere(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	var newBuilder squirrel.UpdateBuilder

	switch l.Get(2).Type() { //nolint:exhaustive // only string/table/userdata types valid
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

	wrapUpdateBuilder(l, &updateBuilderWrapper{builder: newBuilder})
	return 1
}

func updateOrderBy(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	orderBys := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		orderBys = append(orderBys, l.CheckString(i))
	}

	wrapUpdateBuilder(l, &updateBuilderWrapper{
		builder: wrapper.builder.OrderBy(orderBys...),
	})
	return 1
}

func updateLimit(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	limit := l.CheckInt64(2)
	if limit < 0 {
		limit = 0
	}

	wrapUpdateBuilder(l, &updateBuilderWrapper{
		builder: wrapper.builder.Limit(uint64(limit)), //nolint:gosec // validated non-negative
	})
	return 1
}

func updateOffset(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	offset := l.CheckInt64(2)
	if offset < 0 {
		offset = 0
	}

	wrapUpdateBuilder(l, &updateBuilderWrapper{
		builder: wrapper.builder.Offset(uint64(offset)), //nolint:gosec // validated non-negative
	})
	return 1
}

func updateSuffix(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	suffix := l.CheckString(2)
	args := make([]any, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, toGoValue(l.Get(i)))
	}

	wrapUpdateBuilder(l, &updateBuilderWrapper{
		builder: wrapper.builder.Suffix(suffix, args...),
	})
	return 1
}

func updateFrom(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	from := l.CheckString(2)

	wrapUpdateBuilder(l, &updateBuilderWrapper{
		builder: wrapper.builder.From(from),
	})
	return 1
}

func updateFromSelect(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	selectWrapper, ok := ud.Value.(*selectBuilderWrapper)
	if !ok {
		l.ArgError(2, "expected SelectBuilder object")
		return 0
	}

	alias := l.CheckString(3)

	wrapUpdateBuilder(l, &updateBuilderWrapper{
		builder: wrapper.builder.FromSelect(selectWrapper.builder, alias),
	})
	return 1
}

func updatePlaceholderFormat(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	format, ok := ud.Value.(squirrel.PlaceholderFormat)
	if !ok {
		l.ArgError(2, "expected placeholder format")
		return 0
	}

	wrapUpdateBuilder(l, &updateBuilderWrapper{
		builder: wrapper.builder.PlaceholderFormat(format),
	})
	return 1
}

func updateToSQL(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, err.Error()).WithKind(lua.KindInvalid).WithRetryable(false))
		return 2
	}

	l.Push(lua.LString(query))
	l.Push(goArgsToLuaTable(l, args))
	return 2
}

func updateRunWith(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	db := checkDB(l, 2)
	if db == nil {
		return 0
	}

	return newQueryExecutorFromUpdate(l, db, wrapper.builder)
}
