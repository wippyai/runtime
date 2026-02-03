package sql

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

type insertBuilderWrapper struct {
	builder squirrel.InsertBuilder
}

var insertBuilderMethods = map[string]lua.LGoFunc{
	"into":               insertInto,
	"columns":            insertColumns,
	"values":             insertValues,
	"set_map":            insertSetMap,
	"select":             insertSelect,
	"prefix":             insertPrefix,
	"suffix":             insertSuffix,
	"options":            insertOptions,
	"placeholder_format": insertPlaceholderFormat,
	"to_sql":             insertToSQL,
	"run_with":           insertRunWith,
}

var insertBuilderMetamethods = map[string]lua.LGoFunc{
	"__tostring": insertToString,
}

func builderInsert(l *lua.LState) int {
	var tableName string
	if l.GetTop() > 0 {
		tableName = l.CheckString(1)
	}

	wrapper := &insertBuilderWrapper{
		builder: squirrel.Insert(tableName).PlaceholderFormat(squirrel.Question),
	}

	value.PushTypedUserData(l, wrapper, insertBuilderTypeName)
	return 1
}

func checkInsertBuilder(l *lua.LState) *insertBuilderWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*insertBuilderWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected InsertBuilder object")
	return nil
}

func wrapInsertBuilder(l *lua.LState, wrapper *insertBuilderWrapper) {
	value.PushTypedUserData(l, wrapper, insertBuilderTypeName)
}

func insertToString(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("InsertBuilder Error: %v", err)))
		return 1
	}

	l.Push(lua.LString(fmt.Sprintf("InsertBuilder: %s [Args: %v]", query, args)))
	return 1
}

func insertInto(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)
	wrapInsertBuilder(l, &insertBuilderWrapper{
		builder: wrapper.builder.Into(table),
	})
	return 1
}

func insertColumns(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	columns := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		columns = append(columns, l.CheckString(i))
	}

	wrapInsertBuilder(l, &insertBuilderWrapper{
		builder: wrapper.builder.Columns(columns...),
	})
	return 1
}

func insertValues(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	values := make([]any, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		v := l.Get(i)
		if v.Type() == lua.LTUserData {
			if ud, ok := v.(*lua.LUserData); ok {
				if ud.Value == "SQL_NULL" {
					values = append(values, nil)
					continue
				}
				if typed, ok := ud.Value.(*TypedValue); ok {
					values = append(values, typed.Value)
					continue
				}
			}
		}
		values = append(values, toGoValue(v))
	}

	wrapInsertBuilder(l, &insertBuilderWrapper{
		builder: wrapper.builder.Values(values...),
	})
	return 1
}

func insertSetMap(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckTable(2)
	valuesMap := luaTableToMap(table)

	wrapInsertBuilder(l, &insertBuilderWrapper{
		builder: wrapper.builder.SetMap(valuesMap),
	})
	return 1
}

func insertSelect(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	selectWrapper, ok := ud.Value.(*selectBuilderWrapper)
	if !ok {
		l.ArgError(2, "expected SelectBuilder object")
		return 0
	}

	wrapInsertBuilder(l, &insertBuilderWrapper{
		builder: wrapper.builder.Select(selectWrapper.builder),
	})
	return 1
}

func insertPrefix(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	prefix := l.CheckString(2)
	args := make([]any, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, toGoValue(l.Get(i)))
	}

	wrapInsertBuilder(l, &insertBuilderWrapper{
		builder: wrapper.builder.Prefix(prefix, args...),
	})
	return 1
}

func insertSuffix(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	suffix := l.CheckString(2)
	args := make([]any, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, toGoValue(l.Get(i)))
	}

	wrapInsertBuilder(l, &insertBuilderWrapper{
		builder: wrapper.builder.Suffix(suffix, args...),
	})
	return 1
}

func insertOptions(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	options := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		options = append(options, l.CheckString(i))
	}

	wrapInsertBuilder(l, &insertBuilderWrapper{
		builder: wrapper.builder.Options(options...),
	})
	return 1
}

func insertPlaceholderFormat(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	format, ok := ud.Value.(squirrel.PlaceholderFormat)
	if !ok {
		l.ArgError(2, "expected placeholder format")
		return 0
	}

	wrapInsertBuilder(l, &insertBuilderWrapper{
		builder: wrapper.builder.PlaceholderFormat(format),
	})
	return 1
}

func insertToSQL(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
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

func insertRunWith(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	if db, ok := ud.Value.(*DB); ok {
		return newQueryExecutorFromInsert(l, db, wrapper.builder)
	}
	if tx, ok := ud.Value.(*Transaction); ok {
		return newQueryExecutorFromInsertTx(l, tx, wrapper.builder)
	}
	l.ArgError(2, "database or transaction expected")
	return 0
}
