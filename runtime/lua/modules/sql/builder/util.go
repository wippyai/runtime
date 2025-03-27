package builder

import (
	"database/sql"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
)

// Row is a wrapper around Squirrel's RowScanner
type Row struct {
	squirrel.RowScanner
	err error
}

// Scan implements the squirrel.RowScanner interface
func (r *Row) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	return r.RowScanner.Scan(dest...)
}

// rowsToTable converts SQL rows to a Lua table
// This function should be imported from the parent sql module
// but is declared here for reference
func rowsToTable(l *lua.LState, rows *sql.Rows) (*lua.LTable, error) {
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get column names: %w", err)
	}

	// Create a slice of interface{} to hold the values for each row
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range columns {
		valuePtrs[i] = &values[i]
	}

	// Create result table
	resultTable := l.CreateTable(0, 0)
	rowIndex := 1

	// Iterate through rows
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}

		// Create a table for this row
		rowTable := l.CreateTable(0, len(columns))

		// Add columns to row table
		for i, colName := range columns {
			val := values[i]

			// Process different types of values
			switch v := val.(type) {
			case nil:
				rowTable.RawSetString(colName, lua.LNil)
			case []byte:
				// Convert byte slices to strings
				rowTable.RawSetString(colName, lua.LString(string(v)))
			case int64:
				rowTable.RawSetString(colName, lua.LNumber(v))
			case float64:
				rowTable.RawSetString(colName, lua.LNumber(v))
			case bool:
				rowTable.RawSetString(colName, lua.LBool(v))
			case string:
				rowTable.RawSetString(colName, lua.LString(v))
			default:
				// Use string representation for other types
				rowTable.RawSetString(colName, lua.LString(fmt.Sprintf("%v", v)))
			}
		}

		// Add row to result table
		resultTable.RawSetInt(rowIndex, rowTable)
		rowIndex++
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return resultTable, nil
}

// executeQuery is a helper function to execute a query with a Sqlizer
func executeQuery(l *lua.LState, sqlizer squirrel.Sqlizer, runner squirrel.BaseRunner) int {
	coroutine.Wrap(l, func() *engine.Update {
		rows, err := squirrel.QueryWith(runner, sqlizer)
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

// executeQueryRow is a helper function to execute a query row with a Sqlizer
func executeQueryRow(l *lua.LState, sqlizer squirrel.Sqlizer, runner squirrel.QueryRower) int {
	coroutine.Wrap(l, func() *engine.Update {
		row := squirrel.QueryRowWith(runner, sqlizer)
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

// executeStatement is a helper function to execute a statement with a Sqlizer
func executeStatement(l *lua.LState, sqlizer squirrel.Sqlizer, runner squirrel.BaseRunner) int {
	coroutine.Wrap(l, func() *engine.Update {
		result, err := squirrel.ExecWith(runner, sqlizer)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Create result table
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

// registerRowFunctions registers functions for Row objects
func registerRowFunctions(l *lua.LState) {
	// Create Row metatable
	mt := l.CreateTable(0, 1)
	methods := l.CreateTable(0, 1)

	// Add scan method
	methods.RawSetString("scan", l.NewFunction(rowScan))
	mt.RawSetString("__index", methods)

	// Register the metatable
	l.SetGlobal("row_metatable", mt)
}

// rowScan implements the scan method for Row objects
func rowScan(l *lua.LState) int {
	// Check Row object
	ud := l.CheckUserData(1)
	row, ok := ud.Value.(*Row)
	if !ok {
		l.ArgError(1, "expected Row object")
		return 0
	}

	// We need variable references to scan into
	// This is a simplistic implementation that only scans into Lua variables
	// by creating temporary Go variables
	numArgs := l.GetTop() - 1
	if numArgs <= 0 {
		l.ArgError(0, "expected at least one variable to scan into")
		return 0
	}

	// Create slice to hold scanned values
	values := make([]interface{}, numArgs)
	for i := 0; i < numArgs; i++ {
		values[i] = new(interface{})
	}

	// Scan row into values
	err := row.Scan(values...)
	if err != nil {
		l.Push(lua.LBool(false))
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert and push scanned values onto the stack
	for i, val := range values {
		value := *(val.(*interface{}))
		l.Push(goToLuaValue(l, value))
	}

	// Return success + values
	return numArgs + 1
}
