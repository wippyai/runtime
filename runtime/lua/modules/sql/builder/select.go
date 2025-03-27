package builder

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
)

// selectBuilderWrapper wraps a Squirrel SelectBuilder
type selectBuilderWrapper struct {
	builder squirrel.SelectBuilder
}

// builderSelect creates a new select builder
// Usage: sql.builder.select("id", "name", "email")
func builderSelect(l *lua.LState) int {
	// Get columns from arguments - can be strings or expressions
	columns := make([]interface{}, 0, l.GetTop())

	for i := 1; i <= l.GetTop(); i++ {
		arg := l.Get(i)

		switch v := arg.(type) {
		case lua.LString:
			columns = append(columns, string(v))
		case *lua.LUserData:
			// Check if it's a Sqlizer
			if sqlizer, ok := v.Value.(squirrel.Sqlizer); ok {
				columns = append(columns, sqlizer)
			} else {
				l.ArgError(i, "expected string or Sqlizer expression")
				return 0
			}
		default:
			l.ArgError(i, "expected string or Sqlizer expression")
			return 0
		}
	}

	// Create wrapper with default placeholder format
	wrapper := &selectBuilderWrapper{
		builder: squirrel.Select(columns...).PlaceholderFormat(squirrel.Question),
	}

	// Create userdata and set metatable
	ud := l.NewUserData()
	ud.Value = wrapper
	l.SetMetatable(ud, getSelectBuilderMetatable(l))

	l.Push(ud)
	return 1
}

// getSelectBuilderMetatable returns the metatable for SelectBuilder objects
func getSelectBuilderMetatable(l *lua.LState) *lua.LTable {
	// Create method table
	methods := l.CreateTable(0, 20) // Reserve space for all methods

	// Register all the methods
	methods.RawSetString("from", l.NewFunction(selectFrom))
	methods.RawSetString("join", l.NewFunction(selectJoin))
	methods.RawSetString("left_join", l.NewFunction(selectLeftJoin))
	methods.RawSetString("right_join", l.NewFunction(selectRightJoin))
	methods.RawSetString("inner_join", l.NewFunction(selectInnerJoin))
	methods.RawSetString("where", l.NewFunction(selectWhere))
	methods.RawSetString("order_by", l.NewFunction(selectOrderBy))
	methods.RawSetString("group_by", l.NewFunction(selectGroupBy))
	methods.RawSetString("having", l.NewFunction(selectHaving))
	methods.RawSetString("limit", l.NewFunction(selectLimit))
	methods.RawSetString("offset", l.NewFunction(selectOffset))
	methods.RawSetString("columns", l.NewFunction(selectColumns))
	methods.RawSetString("distinct", l.NewFunction(selectDistinct))
	methods.RawSetString("suffix", l.NewFunction(selectSuffix))
	methods.RawSetString("run_with", l.NewFunction(selectRunWith))
	methods.RawSetString("placeholder_format", l.NewFunction(selectPlaceholderFormat))
	methods.RawSetString("to_sql", l.NewFunction(selectToSql))
	methods.RawSetString("query", l.NewFunction(selectQuery))
	methods.RawSetString("query_row", l.NewFunction(selectQueryRow))
	methods.RawSetString("scan", l.NewFunction(selectScan))

	// Create metatable with __index and __tostring
	mt := l.CreateTable(0, 2)
	mt.RawSetString("__index", methods)
	mt.RawSetString("__tostring", l.NewFunction(func(l *lua.LState) int {
		wrapper := checkSelectBuilder(l)
		if wrapper == nil {
			l.Push(lua.LString("Invalid SelectBuilder"))
			return 1
		}

		// Get SQL for display
		query, args, err := wrapper.builder.ToSql()
		if err != nil {
			l.Push(lua.LString(fmt.Sprintf("SelectBuilder Error: %v", err)))
			return 1
		}

		l.Push(lua.LString(fmt.Sprintf("SelectBuilder: %s [Args: %v]", query, args)))
		return 1
	}))

	return mt
}

// checkSelectBuilder ensures the first argument is a SelectBuilder and returns it
func checkSelectBuilder(l *lua.LState) *selectBuilderWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*selectBuilderWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected SelectBuilder object")
	return nil
}

// Method implementations for SelectBuilder

// selectFrom implements the FROM clause
// Usage: builder:from("users")
func selectFrom(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	table := l.CheckString(2)
	wrapper.builder = wrapper.builder.From(table)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectWhere implements the WHERE clause
// Usage: builder:where("id > ?", 100) or builder:where({active = true})
func selectWhere(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
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

// selectJoin implements JOIN clauses
// Usage: builder:join("emails USING (email_id)")
func selectJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaToGoValue(l, l.Get(i)))
	}

	wrapper.builder = wrapper.builder.Join(join, args...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectLeftJoin implements LEFT JOIN clauses
// Usage: builder:left_join("emails USING (email_id)")
func selectLeftJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaToGoValue(l, l.Get(i)))
	}

	wrapper.builder = wrapper.builder.LeftJoin(join, args...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectRightJoin implements RIGHT JOIN clauses
// Usage: builder:right_join("emails USING (email_id)")
func selectRightJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaToGoValue(l, l.Get(i)))
	}

	wrapper.builder = wrapper.builder.RightJoin(join, args...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectInnerJoin implements INNER JOIN clauses
// Usage: builder:inner_join("emails USING (email_id)")
func selectInnerJoin(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	join := l.CheckString(2)

	// Handle optional args
	args := make([]interface{}, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		args = append(args, luaToGoValue(l, l.Get(i)))
	}

	wrapper.builder = wrapper.builder.InnerJoin(join, args...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectOrderBy implements ORDER BY clause
// Usage: builder:order_by("name ASC", "id DESC")
func selectOrderBy(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
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

// selectGroupBy implements GROUP BY clause
// Usage: builder:group_by("department", "location")
func selectGroupBy(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Collect all group by clauses
	groupBys := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		groupBys = append(groupBys, l.CheckString(i))
	}

	wrapper.builder = wrapper.builder.GroupBy(groupBys...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectHaving implements HAVING clause
// Usage: builder:having("COUNT(*) > ?", 5)
func selectHaving(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Handle different types of having conditions (similar to where)
	switch l.Get(2).Type() {
	case lua.LTString:
		// String condition with args
		condition := l.CheckString(2)
		args := make([]interface{}, 0, l.GetTop()-2)
		for i := 3; i <= l.GetTop(); i++ {
			args = append(args, luaToGoValue(l, l.Get(i)))
		}
		wrapper.builder = wrapper.builder.Having(condition, args...)

	case lua.LTTable:
		// Table condition
		table := l.CheckTable(2)
		eqMap := luaTableToMap(l, table)
		wrapper.builder = wrapper.builder.Having(squirrel.Eq(eqMap))

	case lua.LTUserData:
		// Sqlizer condition
		ud := l.CheckUserData(2)
		if sqlizer, ok := ud.Value.(squirrel.Sqlizer); ok {
			wrapper.builder = wrapper.builder.Having(sqlizer)
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

// selectLimit implements LIMIT clause
// Usage: builder:limit(10)
func selectLimit(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	limit := l.CheckNumber(2)
	wrapper.builder = wrapper.builder.Limit(uint64(limit))

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectOffset implements OFFSET clause
// Usage: builder:offset(20)
func selectOffset(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	offset := l.CheckNumber(2)
	wrapper.builder = wrapper.builder.Offset(uint64(offset))

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectColumns adds additional columns to the SELECT
// Usage: builder:columns("count(*) as total")
func selectColumns(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Get columns from arguments - can be strings or expressions
	columns := make([]interface{}, 0, l.GetTop()-1)

	for i := 2; i <= l.GetTop(); i++ {
		arg := l.Get(i)

		switch v := arg.(type) {
		case lua.LString:
			columns = append(columns, string(v))
		case *lua.LUserData:
			// Check if it's a Sqlizer
			if sqlizer, ok := v.Value.(squirrel.Sqlizer); ok {
				columns = append(columns, sqlizer)
			} else {
				l.ArgError(i, "expected string or Sqlizer expression")
				return 0
			}
		default:
			l.ArgError(i, "expected string or Sqlizer expression")
			return 0
		}
	}

	wrapper.builder = wrapper.builder.Columns(columns...)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectDistinct adds DISTINCT to the SELECT
// Usage: builder:distinct()
func selectDistinct(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	wrapper.builder = wrapper.builder.Distinct()

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectSuffix adds a suffix to the SELECT
// Usage: builder:suffix("FOR UPDATE")
func selectSuffix(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
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

// selectPlaceholderFormat sets the placeholder format
// Usage: builder:placeholder_format(sql.builder.dollar)
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

	wrapper.builder = wrapper.builder.PlaceholderFormat(format)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectRunWith sets the runner for query execution
// Usage: builder:run_with(db)
func selectRunWith(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check for DB or Transaction
	ud := l.CheckUserData(2)

	var runner squirrel.BaseRunner

	// Extract the SQL runner from our DB or Transaction types
	switch v := ud.Value.(type) {
	case *DB:
		runner = v.db
	case *Transaction:
		runner = v.tx
	default:
		l.ArgError(2, "expected database or transaction")
		return 0
	}

	wrapper.builder = wrapper.builder.RunWith(runner)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// selectToSql generates the SQL and args
// Usage: sql, args = builder:to_sql()
func selectToSql(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
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

// selectQuery executes the query and returns rows
// Usage: rows, err = builder:query()
func selectQuery(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
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

		// Convert rows to Lua table using your existing rowsToTable
		resultTable, err := rowsToTable(l, rows)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield to coroutine
}

// selectQueryRow executes the query and returns a single row
// Usage: row, err = builder:query_row()
func selectQueryRow(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
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

// selectScan executes the query and scans into variables
// Usage: builder:scan(var1, var2, var3)
func selectScan(l *lua.LState) int {
	wrapper := checkSelectBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Check if we have at least one variable to scan into
	if l.GetTop() < 2 {
		l.ArgError(0, "expected at least one variable to scan into")
		return 0
	}

	// Check if runner is set
	if wrapper.builder.RunWith == nil {
		l.Push(lua.LBool(false))
		l.Push(lua.LString(RunnerNotSet))
		return 2
	}

	// This is complex - we need to create Lua references to update later
	// Not implementing full scan support in this initial version
	l.RaiseError("scan method not fully implemented - use query_row() instead")
	return 0
}
