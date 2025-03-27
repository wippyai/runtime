package builder

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
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

	// Create userdata and set metatable
	ud := l.NewUserData()
	ud.Value = wrapper
	l.SetMetatable(ud, getDeleteBuilderMetatable(l))

	l.Push(ud)
	return 1
}

// getDeleteBuilderMetatable returns the metatable for DeleteBuilder objects
func getDeleteBuilderMetatable(l *lua.LState) *lua.LTable {
	// Create method table
	methods := l.CreateTable(0, 12) // Reserve space for all methods

	// Register all the methods
	methods.RawSetString("from", l.NewFunction(deleteFrom))
	methods.RawSetString("where", l.NewFunction(deleteWhere))
	methods.RawSetString("order_by", l.NewFunction(deleteOrderBy))
	methods.RawSetString("limit", l.NewFunction(deleteLimit))
	methods.RawSetString("offset", l.NewFunction(deleteOffset))
	methods.RawSetString("suffix", l.NewFunction(deleteSuffix))
	methods.RawSetString("run_with", l.NewFunction(deleteRunWith))
	methods.RawSetString("placeholder_format", l.NewFunction(deletePlaceholderFormat))
	methods.RawSetString("to_sql", l.NewFunction(deleteToSql))
	methods.RawSetString("exec", l.NewFunction(deleteExec))

	// Create metatable with __index and __tostring
	mt := l.CreateTable(0, 2)
	mt.RawSetString("__index", methods)
	mt.RawSetString("__tostring", l.NewFunction(func(l *lua.LState) int {
		wrapper := checkDeleteBuilder(l)
		if wrapper == nil {
			l.Push(lua.LString("Invalid DeleteBuilder"))
			return 1
		}

		// Get SQL for display
		query, args, err := wrapper.builder.ToSql()
		if err != nil {
			l.Push(lua.LString(fmt.Sprintf("DeleteBuilder Error: %v", err)))
			return 1
		}

		l.Push(lua.LString(fmt.Sprintf("DeleteBuilder: %s [Args: %v]", query, args)))
		return 1
	}))

	return mt
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

// Method implementations for DeleteBuilder

// deleteFrom sets the table to delete from
// Usage: builder:from("users")
func deleteFrom(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)
	wrapper.builder = wrapper.builder.From(table)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// deleteWhere adds a WHERE condition
// Usage: builder:where({id = 1}) or builder:where("id > ?", 100)
func deleteWhere(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
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

// deleteOrderBy adds an ORDER BY clause
// Usage: builder:order_by("id DESC")
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

	wrapper.builder = wrapper.builder.OrderBy(orderBys...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// deleteLimit adds a LIMIT clause
// Usage: builder:limit(10)
func deleteLimit(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	limit := l.CheckNumber(2)
	wrapper.builder = wrapper.builder.Limit(uint64(limit))

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// deleteOffset adds an OFFSET clause
// Usage: builder:offset(20)
func deleteOffset(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
	if wrapper == nil {
		return 0
	}

	offset := l.CheckNumber(2)
	wrapper.builder = wrapper.builder.Offset(uint64(offset))

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// deleteSuffix adds a suffix to the query
// Usage: builder:suffix("RETURNING id")
func deleteSuffix(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
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

// deletePlaceholderFormat sets the placeholder format
// Usage: builder:placeholder_format(sql.builder.dollar)
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

	wrapper.builder = wrapper.builder.PlaceholderFormat(format)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// deleteRunWith sets the runner for query execution
// Usage: builder:run_with(db)
func deleteRunWith(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
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

// deleteToSql generates the SQL and args
// Usage: sql, args = builder:to_sql()
func deleteToSql(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
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

// deleteExec executes the query
// Usage: result, err = builder:exec()
func deleteExec(l *lua.LState) int {
	wrapper := checkDeleteBuilder(l)
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

		// Get last insert ID (usually not relevant for DELETE but included for consistency)
		if lastInsertID, err := result.LastInsertId(); err == nil {
			resultTable.RawSetString("last_insert_id", lua.LNumber(lastInsertID))
		} else {
			resultTable.RawSetString("last_insert_id", lua.LNil)
		}

		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield to coroutine
}
