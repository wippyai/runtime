package builder

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
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

	// Create userdata and set metatable
	ud := l.NewUserData()
	ud.Value = wrapper
	l.SetMetatable(ud, getUpdateBuilderMetatable(l))

	l.Push(ud)
	return 1
}

// getUpdateBuilderMetatable returns the metatable for UpdateBuilder objects
func getUpdateBuilderMetatable(l *lua.LState) *lua.LTable {
	// Create method table
	methods := l.CreateTable(0, 15) // Reserve space for all methods

	// Register all the methods
	methods.RawSetString("table", l.NewFunction(updateTable))
	methods.RawSetString("set", l.NewFunction(updateSet))
	methods.RawSetString("set_map", l.NewFunction(updateSetMap))
	methods.RawSetString("where", l.NewFunction(updateWhere))
	methods.RawSetString("order_by", l.NewFunction(updateOrderBy))
	methods.RawSetString("limit", l.NewFunction(updateLimit))
	methods.RawSetString("offset", l.NewFunction(updateOffset))
	methods.RawSetString("suffix", l.NewFunction(updateSuffix))
	methods.RawSetString("from", l.NewFunction(updateFrom))
	methods.RawSetString("from_select", l.NewFunction(updateFromSelect))
	methods.RawSetString("run_with", l.NewFunction(updateRunWith))
	methods.RawSetString("placeholder_format", l.NewFunction(updatePlaceholderFormat))
	methods.RawSetString("to_sql", l.NewFunction(updateToSql))
	methods.RawSetString("exec", l.NewFunction(updateExec))
	methods.RawSetString("query", l.NewFunction(updateQuery))
	methods.RawSetString("query_row", l.NewFunction(updateQueryRow))

	// Create metatable with __index and __tostring
	mt := l.CreateTable(0, 2)
	mt.RawSetString("__index", methods)
	mt.RawSetString("__tostring", l.NewFunction(func(l *lua.LState) int {
		wrapper := checkUpdateBuilder(l)
		if wrapper == nil {
			l.Push(lua.LString("Invalid UpdateBuilder"))
			return 1
		}

		// Get SQL for display
		query, args, err := wrapper.builder.ToSql()
		if err != nil {
			l.Push(lua.LString(fmt.Sprintf("UpdateBuilder Error: %v", err)))
			return 1
		}

		l.Push(lua.LString(fmt.Sprintf("UpdateBuilder: %s [Args: %v]", query, args)))
		return 1
	}))

	return mt
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

// Method implementations for UpdateBuilder

// updateTable sets the table to update
// Usage: builder:table("users")
func updateTable(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)
	wrapper.builder = wrapper.builder.Table(table)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// updateSet sets a column to a value
// Usage: builder:set("name", "John")
func updateSet(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Get column and value
	column := l.CheckString(2)
	value := luaToGoValue(l, l.Get(3))

	wrapper.builder = wrapper.builder.Set(column, value)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// updateSetMap sets multiple columns from a map
// Usage: builder:set_map({name = "John", email = "john@example.com"})
func updateSetMap(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
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

// updateWhere adds a WHERE condition
// Usage: builder:where({id = 1}) or builder:where("id > ?", 100)
func updateWhere(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Handle different types of where conditions
	switch l.Get(2).Type() {
	case lua.LTString:
		// String condition with args: where("id > ?", 100)
		condition := l.CheckString(2)
		args := make([]interface{}, 0, l.GetTop()-2)
		for i := 3; i <= l.GetTop(); i++ {
			args = append(args, luaToGoValue(l, l.Get(i)))
		}
		wrapper.builder = wrapper.builder.Where(condition, args...)

	case lua.LTTable:
		// Table condition: where({active = true})
		table := l.CheckTable(2)
		eqMap := luaTableToMap(l, table)
		wrapper.builder = wrapper.builder.Where(squirrel.Eq(eqMap))

	case lua.LTUserData:
		// Sqlizer condition: where(sql.builder.eq({...}))
		ud := l.CheckUserData(2)
		if sqlizer, ok := ud.Value.(squirrel.Sqlizer); ok {
			wrapper.builder = wrapper.builder.Where(sqlizer)
		} else {
			l.ArgError(2, "expected string, table, or Sqlizer")
			return 0
		}

	default:
		l.ArgError(2, "expected string, table, or Sqlizer")
		return 0
	}

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// updateOrderBy adds an ORDER BY clause
// Usage: builder:order_by("id DESC")
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

	wrapper.builder = wrapper.builder.OrderBy(orderBys...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// updateLimit adds a LIMIT clause
// Usage: builder:limit(10)
func updateLimit(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	limit := l.CheckNumber(2)
	wrapper.builder = wrapper.builder.Limit(uint64(limit))

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// updateOffset adds an OFFSET clause
// Usage: builder:offset(20)
func updateOffset(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	offset := l.CheckNumber(2)
	wrapper.builder = wrapper.builder.Offset(uint64(offset))

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// updateSuffix adds a suffix to the query
// Usage: builder:suffix("RETURNING id")
func updateSuffix(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
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

// updateFrom adds a FROM clause (for Postgres)
// Usage: builder:from("other_table")
func updateFrom(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	from := l.CheckString(2)
	wrapper.builder = wrapper.builder.From(from)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// updateFromSelect adds a FROM (SELECT...) clause
// Usage: builder:from_select(selectBuilder, "sub")
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
	wrapper.builder = wrapper.builder.FromSelect(selectWrapper.builder, alias)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// updatePlaceholderFormat sets the placeholder format
// Usage: builder:placeholder_format(sql.builder.dollar)
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

	wrapper.builder = wrapper.builder.PlaceholderFormat(format)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// updateRunWith sets the runner for query execution
// Usage: builder:run_with(db)
func updateRunWith(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
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
		argsTable.RawSetInt(i+1, goToLuaValue(l, arg))
	}

	l.Push(argsTable)
	return 2
}

// updateExec executes the query
// Usage: result, err = builder:exec()
func updateExec(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check if runner is set
	if wrapper.builder.RunWith == nil {
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

		// Get last insert ID (usually not relevant for UPDATE but included for consistency)
		if lastInsertID, err := result.LastInsertId(); err == nil {
			resultTable.RawSetString("last_insert_id", lua.LNumber(lastInsertID))
		} else {
			resultTable.RawSetString("last_insert_id", lua.LNil)
		}

		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield to coroutine
}

// updateQuery executes the query and returns rows
// Usage: rows, err = builder:query()
func updateQuery(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check if runner is set
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

// updateQueryRow executes the query and returns a single row
// Usage: row, err = builder:query_row()
func updateQueryRow(l *lua.LState) int {
	wrapper := checkUpdateBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check if runner is set
	if wrapper.builder.RunWith == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(RunnerNotSet))
		return 2
	}

	// Use coroutine for async execution
	coroutine.Wrap(l, func() *engine.Update {
		row := wrapper.builder.QueryRow()
		if row == nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("query failed")}, nil)
		}

		// Create a Row object
		rowObj := &Row{RowScanner: row, err: nil}

		// Create Lua userdata with appropriate metatable
		ud := l.NewUserData()
		ud.Value = rowObj

		return engine.NewUpdate(nil, []lua.LValue{ud, lua.LNil}, nil)
	})

	return -1 // Yield to coroutine
}
