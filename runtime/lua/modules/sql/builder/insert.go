package builder

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
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

	// Create userdata and set metatable
	ud := wrapInsertBuilder(l, wrapper)
	l.Push(ud)
	return 1
}

// wrapInsertBuilder wraps an InsertBuilder into a Lua userdata with the proper metatable
func wrapInsertBuilder(l *lua.LState, wrapper *insertBuilderWrapper) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = value.GetTypeMetatable(l, "sql.InsertBuilder")
	return ud
}

// registerInsertBuilderType registers the InsertBuilder type metatable
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
		"run_with":           insertRunWith,
		"placeholder_format": insertPlaceholderFormat,
		"to_sql":             insertToSql,
		"exec":               insertExec,
		"query":              insertQuery,
		"query_one":          insertQueryOne,
	}

	// Define metamethods
	metamethods := map[string]lua.LGFunction{
		"__tostring": insertToString,
	}

	// Register the type
	value.RegisterTypeMethods(l, "sql.InsertBuilder", metamethods, methods)
}

// insertToString is the __tostring metamethod for InsertBuilder
func insertToString(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		l.Push(lua.LString("Invalid InsertBuilder"))
		return 1
	}

	// Get SQL for display
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

// Method implementations for InsertBuilder

// insertInto sets the table to insert into
// Usage: builder:into("users")
func insertInto(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)
	wrapper.builder = wrapper.builder.Into(table)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// insertColumns sets the columns to insert into
// Usage: builder:columns("id", "name", "email")
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

	wrapper.builder = wrapper.builder.Columns(columns...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// insertValues adds a row of values
// Usage: builder:values(1, "John", "john@example.com")
func insertValues(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Convert Lua values to Go values
	values := make([]interface{}, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		values = append(values, luaToGoValue(l, l.Get(i)))
	}

	wrapper.builder = wrapper.builder.Values(values...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// insertSetMap sets columns and values from a map
// Usage: builder:set_map({id = 1, name = "John", email = "john@example.com"})
func insertSetMap(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Convert Lua table to Go map
	table := l.CheckTable(2)
	valuesMap := luaTableToMap(l, table)

	wrapper.builder = wrapper.builder.SetMap(valuesMap)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// insertSelect sets a SELECT query as the source of values
// Usage: builder:select(selectBuilder)
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

	wrapper.builder = wrapper.builder.Select(selectWrapper.builder)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// insertSuffix adds a suffix to the query
// Usage: builder:suffix("ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)")
func insertSuffix(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	suffix := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaToGoValue(l, l.Get(i)))
	}

	wrapper.builder = wrapper.builder.Suffix(suffix, args...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// insertOptions adds options to the INSERT statement
// Usage: builder:options("IGNORE")
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

	wrapper.builder = wrapper.builder.Options(options...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// insertPlaceholderFormat sets the placeholder format
// Usage: builder:placeholder_format(sql.builder.dollar)
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

	wrapper.builder = wrapper.builder.PlaceholderFormat(format)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// insertRunWith sets the runner for query execution
// Usage: builder:run_with(db)
func insertRunWith(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check for DB or Transaction
	ud := l.CheckUserData(2)
	runner, err := getBaseRunner(l, ud.Value)
	if err != nil {
		l.ArgError(2, err.Error())
		return 0
	}

	wrapper.builder = wrapper.builder.RunWith(runner)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// insertToSql generates the SQL and args
// Usage: sql, args = builder:to_sql()
func insertToSql(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
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
		argsTable.RawSetInt(i+1, goToLuaValue(l, arg))
	}

	l.Push(argsTable)
	return 2
}

// insertExec executes the query
// Usage: result, err = builder:exec()
func insertExec(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Ensure runner is set
	if wrapper.builder.RunWith == nil { // todo: this is wrong
		l.Push(lua.LNil)
		l.Push(lua.LString(RunnerNotSet))
		return 2
	}

	// Use coroutine for async execution
	coroutine.Wrap(l, func() *engine.Update {
		result, err := wrapper.builder.Exec()
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Convert result to Lua table
		resultTable := l.CreateTable(0, 2)

		// Get rows affected
		if rowsAffected, err := result.RowsAffected(); err == nil {
			resultTable.RawSetString("rows_affected", lua.LNumber(rowsAffected))
		} else {
			resultTable.RawSetString("rows_affected", lua.LNil)
		}

		// Get last insert ID
		if lastInsertID, err := result.LastInsertId(); err == nil {
			resultTable.RawSetString("last_insert_id", lua.LNumber(lastInsertID))
		} else {
			resultTable.RawSetString("last_insert_id", lua.LNil)
		}

		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield to coroutine
}

// insertQuery executes the query and returns rows
// Usage: rows, err = builder:query()
func insertQuery(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Ensure runner is set
	if wrapper.builder.RunWith == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(RunnerNotSet))
		return 2
	}

	// Use coroutine for async execution
	coroutine.Wrap(l, func() *engine.Update {
		rows, err := wrapper.builder.Query()
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Convert rows to Lua table
		resultTable, err := rowsToTable(l, rows)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield to coroutine
}

// insertQueryOne executes the query and returns a single row directly as a table
// Usage: row, err = builder:query_one()
func insertQueryOne(l *lua.LState) int {
	wrapper := checkInsertBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Ensure runner is set
	if wrapper.builder.RunWith == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(RunnerNotSet))
		return 2
	}

	// Use coroutine for async execution
	coroutine.Wrap(l, func() *engine.Update {
		// Get the row
		row := wrapper.builder.QueryRow()
		if row == nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("query failed")}, nil)
		}

		// Convert the single row to a Lua table
		rowTable, err := scanRowToTable(l, row)
		if err != nil {
			if err.Error() == "sql: no rows in result set" {
				// No rows is a normal case, return nil without error
				return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LNil}, nil)
			}
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{rowTable, lua.LNil}, nil)
	})

	return -1 // Yield to coroutine
}
