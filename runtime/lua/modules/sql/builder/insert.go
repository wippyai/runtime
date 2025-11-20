package builder

import (
	"fmt"

	"github.com/wippyai/runtime/api/service/sql"

	"github.com/Masterminds/squirrel"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// insertBuilderWrapper wraps a Squirrel InsertBuilder
type insertBuilderWrapper struct {
	builder squirrel.InsertBuilder
}

// builderInsert creates a new insert builder
// Usage: sql.builder.insert("table_name")
func builderInsert(l *lua.LState) int {
	// Get table name (optional)
	var tableName string
	if l.GetTop() > 0 {
		tableName = l.CheckString(1)
	}

	// Create wrapper with default placeholder format
	wrapper := &insertBuilderWrapper{
		builder: squirrel.Insert(tableName).PlaceholderFormat(squirrel.Question),
	}

	// Create userdata
	ud := wrapInsertBuilder(l, wrapper)
	l.Push(ud)
	return 1
}

// wrapInsertBuilder wraps an InsertBuilder in a Lua userdata
func wrapInsertBuilder(l *lua.LState, wrapper *insertBuilderWrapper) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = value.GetTypeMetatable(l, "sql.InsertBuilder")
	return ud
}

// registerInsertBuilderType registers the InsertBuilder metatable
func registerInsertBuilderType(l *lua.LState) {
	// Define methods
	methods := map[string]lua.LGFunction{
		"into":               insertInto,
		"columns":            insertColumns,
		"values":             insertValues,
		"set_map":            insertSetMap,
		"select":             insertSelect,
		"suffix":             insertSuffix,
		"options":            insertOptions,
		"placeholder_format": insertPlaceholderFormat,
		"to_sql":             insertToSQL,
		"run_with":           insertRunWith,
	}

	// Define metamethods
	metamethods := map[string]lua.LGFunction{
		"__tostring": insertToString,
	}

	// Register the metatable
	value.RegisterTypeMethods(l, "sql.InsertBuilder", metamethods, methods)
}

// insertToString is the __tostring metamethod for InsertBuilder
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

// checkInsertBuilder ensures the first argument is an InsertBuilder and returns it
func checkInsertBuilder(l *lua.LState) *insertBuilderWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*insertBuilderWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected InsertBuilder object")
	return nil
}

// Method implementations for InsertBuilder (all immutable)

// insertInto sets the table to insert into
// Usage: builder = builder:into("users")
func insertInto(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)

	// Create new wrapper with updated builder
	newWrapper := &insertBuilderWrapper{
		builder: wrapper.builder.Into(table),
	}

	// Return new wrapped builder
	ud := wrapInsertBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// insertColumns sets the columns to insert into
// Usage: builder = builder:columns("id", "name", "email")
func insertColumns(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Get columns from arguments
	columns := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		columns = append(columns, l.CheckString(i))
	}

	// Create new wrapper
	newWrapper := &insertBuilderWrapper{
		builder: wrapper.builder.Columns(columns...),
	}

	// Return new wrapped builder
	ud := wrapInsertBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// insertValues adds a row of values
// Usage: builder = builder:values(1, "John", "john@example.com")
func insertValues(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Convert Lua values to Go values
	values := make([]interface{}, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		values = append(values, value.ToGoAny(l.Get(i)))
	}

	// Create new wrapper
	newWrapper := &insertBuilderWrapper{
		builder: wrapper.builder.Values(values...),
	}

	// Return new wrapped builder
	ud := wrapInsertBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// insertSetMap sets columns and values from a map
// Usage: builder = builder:set_map({id = 1, name = "John", email = "john@example.com"})
func insertSetMap(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Convert Lua table to Go map
	table := l.CheckTable(2)
	valuesMap := luaTableToMap(l, table)

	// Create new wrapper
	newWrapper := &insertBuilderWrapper{
		builder: wrapper.builder.SetMap(valuesMap),
	}

	// Return new wrapped builder
	ud := wrapInsertBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// insertSelect sets a SELECT query as the source of values
// Usage: builder = builder:select(selectBuilder)
func insertSelect(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check for SelectBuilder
	ud := l.CheckUserData(2)
	selectWrapper, ok := ud.Value.(*selectBuilderWrapper)
	if !ok {
		l.ArgError(2, "expected SelectBuilder object")
		return 0
	}

	// Create new wrapper
	newWrapper := &insertBuilderWrapper{
		builder: wrapper.builder.Select(selectWrapper.builder),
	}

	// Return new wrapped builder
	ud = wrapInsertBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// insertSuffix adds a suffix to the query
// Usage: builder = builder:suffix("ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)")
func insertSuffix(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
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
	newWrapper := &insertBuilderWrapper{
		builder: wrapper.builder.Suffix(suffix, args...),
	}

	// Return new wrapped builder
	ud := wrapInsertBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// insertOptions adds options to the INSERT statement
// Usage: builder = builder:options("IGNORE")
func insertOptions(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Get options from arguments
	options := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		options = append(options, l.CheckString(i))
	}

	// Create new wrapper
	newWrapper := &insertBuilderWrapper{
		builder: wrapper.builder.Options(options...),
	}

	// Return new wrapped builder
	ud := wrapInsertBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// insertPlaceholderFormat sets the placeholder format
// Usage: builder = builder:placeholder_format(sql.builder.dollar)
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

	// Create new wrapper
	newWrapper := &insertBuilderWrapper{
		builder: wrapper.builder.PlaceholderFormat(format),
	}

	// Return new wrapped builder
	ud = wrapInsertBuilder(l, newWrapper)
	l.Push(ud)
	return 1
}

// insertToSQL generates the SQL and args
// Usage: sql, args = builder:to_sql()
func insertToSQL(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
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

// insertRunWith creates an executor with this builder
// Usage: executor = builder:run_with(db)
func insertRunWith(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check for DB or Transaction
	ud := l.CheckUserData(2)

	if v, ok := ud.Value.(DBTypeGetter); ok {
		if v.GetDBType() == sql.KindPostgres {
			wrapper = &insertBuilderWrapper{
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
