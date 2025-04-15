package builder

import (
	"fmt"
	"github.com/Masterminds/squirrel"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/modules/sql/sqlutil"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// updateBuilderWrapper wraps a Squirrel UpdateBuilder
type updateBuilderWrapper struct {
	builder squirrel.UpdateBuilder
}

// builderUpdate creates a new update builder
// Usage: sql.builder.update("table_name")
func builderUpdate(l *lua.LState) int {
	// Get table name (optional)
	var tableName string
	if l.GetTop() > 0 {
		tableName = l.CheckString(1)
	}

	// Create wrapper with default placeholder format
	wrapper := &updateBuilderWrapper{
		builder: squirrel.Update(tableName).PlaceholderFormat(squirrel.Question),
	}

	// Create userdata
	ud := wrapUpdateBuilder(l, wrapper)
	l.Push(ud)
	return 1
}

// wrapUpdateBuilder wraps an UpdateBuilder in a Lua userdata
func wrapUpdateBuilder(l *lua.LState, wrapper *updateBuilderWrapper) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = value.GetTypeMetatable(l, "sql.UpdateBuilder")
	return ud
}

// registerUpdateBuilderType registers the UpdateBuilder metatable
func registerUpdateBuilderType(l *lua.LState) {
	// Define methods
	methods := map[string]lua.LGFunction{
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
		"to_sql":             updateToSql,
		"run_with":           updateRunWith,
	}

	// Define metamethods
	metamethods := map[string]lua.LGFunction{
		"__tostring": updateToString,
	}

	// Register the metatable
	value.RegisterTypeMethods(l, "sql.UpdateBuilder", metamethods, methods)
}

// updateToString is the __tostring metamethod for UpdateBuilder
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

// checkUpdateBuilder ensures the first argument is an UpdateBuilder and returns it
func checkUpdateBuilder(l *lua.LState) *updateBuilderWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*updateBuilderWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected UpdateBuilder object")
	return nil
}

// Method implementations (all immutable)

// updateTable sets the table to update
// Usage: builder = builder:table("users")
func updateTable(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{
		builder: wrapper.builder.Table(table),
	}

	// Return new wrapped builder
	ud := wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updateSet sets a column to a value
// Usage: builder = builder:set("name", "John")
func updateSet(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Get column and value
	column := l.CheckString(2)

	p, err := sqlutil.CheckParam(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{
		builder: wrapper.builder.Set(column, p),
	}

	// Return new wrapped builder
	ud := wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updateSetMap sets multiple columns from a map
// Usage: builder = builder:set_map({name = "John", email = "john@example.com"})
func updateSetMap(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Convert Lua table to Go map
	table := l.CheckTable(2)
	valuesMap := luaTableToMap(l, table)

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{
		builder: wrapper.builder.SetMap(valuesMap),
	}

	// Return new wrapped builder
	ud := wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updateWhere adds a WHERE condition
// Usage: builder = builder:where({id = 1}) or builder:where("id > ?", 100)
func updateWhere(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Updated builder to store result
	var newBuilder squirrel.UpdateBuilder

	switch l.Get(2).Type() {
	case lua.LTString:
		// String condition with args: where("id > ?", 100)
		condition := l.CheckString(2)
		args := make([]interface{}, 0, l.GetTop()-2)
		for i := 3; i <= l.GetTop(); i++ {
			args = append(args, luaconv.ToGoAny(l.Get(i)))
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

	default:
		l.ArgError(2, "expected string, table, or Sqlizer")
		return 0
	}

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{builder: newBuilder}

	// Return new wrapped builder
	ud := wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updateOrderBy adds an ORDER BY clause
// Usage: builder = builder:order_by("id DESC")
func updateOrderBy(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Collect all order by clauses
	orderBys := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		orderBys = append(orderBys, l.CheckString(i))
	}

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{
		builder: wrapper.builder.OrderBy(orderBys...),
	}

	// Return new wrapped builder
	ud := wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updateLimit adds a LIMIT clause
// Usage: builder = builder:limit(10)
func updateLimit(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	limit := l.CheckNumber(2)

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{
		builder: wrapper.builder.Limit(uint64(limit)),
	}

	// Return new wrapped builder
	ud := wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updateOffset adds an OFFSET clause
// Usage: builder = builder:offset(20)
func updateOffset(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	offset := l.CheckNumber(2)

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{
		builder: wrapper.builder.Offset(uint64(offset)),
	}

	// Return new wrapped builder
	ud := wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updateSuffix adds a suffix to the query
// Usage: builder = builder:suffix("RETURNING id")
func updateSuffix(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	suffix := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaconv.ToGoAny(l.Get(i)))
	}

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{
		builder: wrapper.builder.Suffix(suffix, args...),
	}

	// Return new wrapped builder
	ud := wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updateFrom adds a FROM clause (for Postgres)
// Usage: builder = builder:from("other_table")
func updateFrom(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	from := l.CheckString(2)

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{
		builder: wrapper.builder.From(from),
	}

	// Return new wrapped builder
	ud := wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updateFromSelect adds a FROM (SELECT...) clause
// Usage: builder = builder:from_select(selectBuilder, "sub")
func updateFromSelect(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check for SelectBuilder and alias
	ud := l.CheckUserData(2)
	selectWrapper, ok := ud.Value.(*selectBuilderWrapper)
	if !ok {
		l.ArgError(2, "expected SelectBuilder object")
		return 0
	}

	alias := l.CheckString(3)

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{
		builder: wrapper.builder.FromSelect(selectWrapper.builder, alias),
	}

	// Return new wrapped builder
	ud = wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updatePlaceholderFormat sets the placeholder format
// Usage: builder = builder:placeholder_format(sql.builder.dollar)
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

	// Create new wrapper
	newWrapper := &updateBuilderWrapper{
		builder: wrapper.builder.PlaceholderFormat(format),
	}

	// Return new wrapped builder
	ud = wrapUpdateBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// updateToSql generates the SQL and args
// Usage: sql, args = builder:to_sql()
func updateToSql(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
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

// updateRunWith creates an executor with this builder
// Usage: executor = builder:run_with(db)
func updateRunWith(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check for DB or Transaction
	ud := l.CheckUserData(2)

	// Create query executor
	executor, err := NewQueryExecutor(l, wrapper.builder, ud.Value)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Return the executor
	l.Push(executor)
	return 1
}
