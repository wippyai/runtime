package sql

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

type selectBuilderWrapper struct {
	builder squirrel.SelectBuilder
}

var selectBuilderMethods = map[string]lua.LGoFunc{
	"from":               selectFrom,
	"join":               selectJoin,
	"left_join":          selectLeftJoin,
	"right_join":         selectRightJoin,
	"inner_join":         selectInnerJoin,
	"where":              selectWhere,
	"order_by":           selectOrderBy,
	"group_by":           selectGroupBy,
	"having":             selectHaving,
	"limit":              selectLimit,
	"offset":             selectOffset,
	"columns":            selectColumns,
	"distinct":           selectDistinct,
	"suffix":             selectSuffix,
	"placeholder_format": selectPlaceholderFormat,
	"to_sql":             selectToSQL,
	"run_with":           selectRunWith,
}

var selectBuilderMetamethods = map[string]lua.LGoFunc{
	"__tostring": selectToString,
}

func builderSelect(l *lua.LState) int {
	columns := make([]string, 0, l.GetTop())
	for i := 1; i <= l.GetTop(); i++ {
		if l.Get(i).Type() != lua.LTString {
			l.ArgError(i, "expected string column name")
			return 0
		}
		columns = append(columns, l.CheckString(i))
	}

	wrapper := &selectBuilderWrapper{
		builder: squirrel.Select(columns...).PlaceholderFormat(squirrel.Question),
	}

	value.PushTypedUserData(l, wrapper, selectBuilderTypeName)
	return 1
}

func checkSelectBuilder(l *lua.LState) *selectBuilderWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*selectBuilderWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected SelectBuilder object")
	return nil
}

func wrapSelectBuilder(l *lua.LState, wrapper *selectBuilderWrapper) {
	value.PushTypedUserData(l, wrapper, selectBuilderTypeName)
}

func selectToString(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("SelectBuilder Error: %v", err)))
		return 1
	}

	l.Push(lua.LString(fmt.Sprintf("SelectBuilder: %s [Args: %v]", query, args)))
	return 1
}

func selectFrom(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)
	newWrapper := &selectBuilderWrapper{
		builder: wrapper.builder.From(table),
	}

	wrapSelectBuilder(l, newWrapper)
	return 1
}

func selectWhere(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	var newBuilder squirrel.SelectBuilder

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

	wrapSelectBuilder(l, &selectBuilderWrapper{builder: newBuilder})
	return 1
}

func selectJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)
	args := make([]any, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, toGoValue(l.Get(i)))
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.Join(join, args...),
	})
	return 1
}

func selectLeftJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)
	args := make([]any, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, toGoValue(l.Get(i)))
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.LeftJoin(join, args...),
	})
	return 1
}

func selectRightJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)
	args := make([]any, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, toGoValue(l.Get(i)))
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.RightJoin(join, args...),
	})
	return 1
}

func selectInnerJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)
	args := make([]any, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, toGoValue(l.Get(i)))
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.InnerJoin(join, args...),
	})
	return 1
}

func selectOrderBy(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	orderBys := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		orderBys = append(orderBys, l.CheckString(i))
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.OrderBy(orderBys...),
	})
	return 1
}

func selectGroupBy(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	groupBys := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		groupBys = append(groupBys, l.CheckString(i))
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.GroupBy(groupBys...),
	})
	return 1
}

func selectHaving(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	var newBuilder squirrel.SelectBuilder

	switch l.Get(2).Type() {
	case lua.LTString:
		condition := l.CheckString(2)
		args := make([]any, 0, l.GetTop()-2)
		for i := 3; i <= l.GetTop(); i++ {
			args = append(args, toGoValue(l.Get(i)))
		}
		newBuilder = wrapper.builder.Having(condition, args...)

	case lua.LTTable:
		table := l.CheckTable(2)
		eqMap := luaTableToMap(table)
		newBuilder = wrapper.builder.Having(squirrel.Eq(eqMap))

	case lua.LTUserData:
		ud := l.CheckUserData(2)
		if sqlizer, ok := ud.Value.(squirrel.Sqlizer); ok {
			newBuilder = wrapper.builder.Having(sqlizer)
		} else {
			l.ArgError(2, "expected string, table, or Sqlizer")
			return 0
		}

	default:
		l.ArgError(2, "expected string, table, or Sqlizer")
		return 0
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{builder: newBuilder})
	return 1
}

func selectLimit(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	limit := l.CheckInt64(2)
	if limit < 0 {
		limit = 0
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.Limit(uint64(limit)), //nolint:gosec // validated non-negative
	})
	return 1
}

func selectOffset(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	offset := l.CheckInt64(2)
	if offset < 0 {
		offset = 0
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.Offset(uint64(offset)), //nolint:gosec // validated non-negative
	})
	return 1
}

func selectColumns(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	columns := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		columns = append(columns, l.CheckString(i))
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.Columns(columns...),
	})
	return 1
}

func selectDistinct(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.Distinct(),
	})
	return 1
}

func selectSuffix(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	suffix := l.CheckString(2)
	args := make([]any, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, toGoValue(l.Get(i)))
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.Suffix(suffix, args...),
	})
	return 1
}

func selectPlaceholderFormat(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	format, ok := ud.Value.(squirrel.PlaceholderFormat)
	if !ok {
		l.ArgError(2, "expected placeholder format")
		return 0
	}

	wrapSelectBuilder(l, &selectBuilderWrapper{
		builder: wrapper.builder.PlaceholderFormat(format),
	})
	return 1
}

func selectToSQL(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
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

func selectRunWith(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	if db, ok := ud.Value.(*DB); ok {
		return newQueryExecutorFromSelect(l, db, wrapper.builder)
	}
	if tx, ok := ud.Value.(*Transaction); ok {
		return newQueryExecutorFromSelectTx(l, tx, wrapper.builder)
	}
	l.ArgError(2, "database or transaction expected")
	return 0
}

func goArgsToLuaTable(l *lua.LState, args []any) *lua.LTable {
	argsTable := l.CreateTable(len(args), 0)
	for i, arg := range args {
		argsTable.RawSetInt(i+1, goValueToLua(l, arg))
	}
	return argsTable
}

func goValueToLua(_ *lua.LState, v any) lua.LValue {
	switch v := v.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(v)
	case int:
		return lua.LInteger(int64(v))
	case int64:
		return lua.LInteger(v)
	case float64:
		return lua.LNumber(v)
	case string:
		return lua.LString(v)
	case []byte:
		return lua.LString(string(v))
	default:
		return lua.LString(fmt.Sprintf("%v", v))
	}
}
