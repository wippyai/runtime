package builder

import (
	"fmt"

	"github.com/wippyai/runtime/api/service/sql"

	"github.com/Masterminds/squirrel"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// deleteBuilderWrapper wraps a Squirrel DeleteBuilder
type deleteBuilderWrapper struct {
	builder squirrel.DeleteBuilder
}

// builderDelete creates a new delete builder
// Usage: sql.builder.delete("table_name")
func builderDelete(l *lua.LState) int {
	// Get table name (optional)
	var tableName string
	if l.GetTop() > 0 {
		tableName = l.CheckString(1)
	}

	// Create wrapper with default placeholder format
	wrapper := &deleteBuilderWrapper{
		builder: squirrel.Delete(tableName).PlaceholderFormat(squirrel.Question),
	}

	// Create userdata
	ud := wrapDeleteBuilder(l, wrapper)
	l.Push(ud)
	return 1
}

// wrapDeleteBuilder wraps a DeleteBuilder in a Lua userdata
func wrapDeleteBuilder(l *lua.LState, wrapper *deleteBuilderWrapper) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = value.GetTypeMetatable(l, "sql.DeleteBuilder")
	return ud
}

// registerDeleteBuilderType registers the DeleteBuilder metatable
func registerDeleteBuilderType(l *lua.LState) {
	// Define methods
	methods := map[string]lua.LGFunction{
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

	// Define metamethods
	metamethods := map[string]lua.LGFunction{
		"__tostring": deleteToString,
	}

	// Register the metatable
	value.RegisterTypeMethods(l, "sql.DeleteBuilder", metamethods, methods)
}

// deleteToString is the __tostring metamethod for DeleteBuilder
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

// checkDeleteBuilder ensures the first argument is a DeleteBuilder and returns it
func checkDeleteBuilder(l *lua.LState) *deleteBuilderWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*deleteBuilderWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected DeleteBuilder object")
	return nil
}

// Method implementations (all immutable)

// deleteFrom sets the table to delete from
// Usage: builder = builder:from("users")
func deleteFrom(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)

	// Create new wrapper
	newWrapper := &deleteBuilderWrapper{
		builder: wrapper.builder.From(table),
	}

	// Return new wrapped builder
	ud := wrapDeleteBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// deleteWhere adds a WHERE condition
// Usage: builder = builder:where({id = 1}) or builder:where("id > ?", 100)
func deleteWhere(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Updated builder to store result
	var newBuilder squirrel.DeleteBuilder

	switch l.Get(2).Type() {
	case lua.LTString:
		// String condition with args: where("id > ?", 100)
		condition := l.CheckString(2)
		args := make([]interface{}, 0, l.GetTop()-2)
		for i := 3; i <= l.GetTop(); i++ {
			args = append(args, value.ToGoAny(l.Get(i)))
		}
		newBuilder = wrapper.builder.Where(condition, args...)

	case lua.LTTable:
		// Table condition: where({active = true})
		table := l.CheckTable(2)
		eqMap := luaTableToMap(l, table)
		newBuilder = wrapper.builder.Where(squirrel.Eq(eqMap))

	case lua.LTUserData:
		// Sqlizer condition: where(sql.builder.eq({...}))
		ud := l.CheckUserData(2)
		if sqlizer, ok := ud.Value.(squirrel.Sqlizer); ok {
			newBuilder = wrapper.builder.Where(sqlizer)
		} else {
			l.ArgError(2, "expected string, table, or Sqlizer")
			return 0
		}
	case lua.LTNil, lua.LTBool, lua.LTNumber, lua.LTFunction, lua.LTThread, lua.LTChannel:
		// FIXME rework on demand
		fallthrough

	default:
		l.ArgError(2, "expected string, table, or Sqlizer")
		return 0
	}

	// Create new wrapper
	newWrapper := &deleteBuilderWrapper{builder: newBuilder}

	// Return new wrapped builder
	ud := wrapDeleteBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// deleteOrderBy adds an ORDER BY clause
// Usage: builder = builder:order_by("id DESC")
func deleteOrderBy(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Collect all order by clauses
	orderBys := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		orderBys = append(orderBys, l.CheckString(i))
	}

	// Create new wrapper
	newWrapper := &deleteBuilderWrapper{
		builder: wrapper.builder.OrderBy(orderBys...),
	}

	// Return new wrapped builder
	ud := wrapDeleteBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// deleteLimit adds a LIMIT clause
// Usage: builder = builder:limit(10)
func deleteLimit(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	limit := l.CheckNumber(2)

	// Create new wrapper
	newWrapper := &deleteBuilderWrapper{
		builder: wrapper.builder.Limit(uint64(limit)),
	}

	// Return new wrapped builder
	ud := wrapDeleteBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// deleteOffset adds an OFFSET clause
// Usage: builder = builder:offset(20)
func deleteOffset(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	offset := l.CheckNumber(2)

	// Create new wrapper
	newWrapper := &deleteBuilderWrapper{
		builder: wrapper.builder.Offset(uint64(offset)),
	}

	// Return new wrapped builder
	ud := wrapDeleteBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// deleteSuffix adds a suffix to the query
// Usage: builder = builder:suffix("RETURNING id")
func deleteSuffix(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	suffix := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, value.ToGoAny(l.Get(i)))
	}

	// Create new wrapper
	newWrapper := &deleteBuilderWrapper{
		builder: wrapper.builder.Suffix(suffix, args...),
	}

	// Return new wrapped builder
	ud := wrapDeleteBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// deletePlaceholderFormat sets the placeholder format
// Usage: builder = builder:placeholder_format(sql.builder.dollar)
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

	// Create new wrapper
	newWrapper := &deleteBuilderWrapper{
		builder: wrapper.builder.PlaceholderFormat(format),
	}

	// Return new wrapped builder
	ud = wrapDeleteBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// deleteToSQL generates the SQL and args
// Usage: sql, args = builder:to_sql()
func deleteToSQL(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newBuilderInvalidError(l, err, "to_sql"))
		return 2
	}

	l.Push(lua.LString(query))

	// Convert args to Lua table
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

// deleteRunWith creates an executor with this builder
// Usage: executor = builder:run_with(db)
func deleteRunWith(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check for DB or Transaction
	ud := l.CheckUserData(2)

	if v, ok := ud.Value.(DBTypeGetter); ok {
		if v.GetDBType() == sql.KindPostgres {
			wrapper = &deleteBuilderWrapper{
				builder: wrapper.builder.PlaceholderFormat(squirrel.Dollar),
			}
		}
	}

	// Create query executor
	executor, err := NewQueryExecutor(l, wrapper.builder, ud.Value)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newBuilderOperationError(l, err, "run_with"))
		return 2
	}

	// Return the executor
	l.Push(executor)
	return 1
}
