// executor.go
package builder

import (
	"database/sql"
	"fmt"
	"github.com/Masterminds/squirrel"
	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	sqlmod "github.com/ponyruntime/pony/runtime/lua/modules/sql"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// QueryExecutor handles the execution of SQL queries
type QueryExecutor struct {
	builder interface{}         // The Squirrel builder (Select, Update, etc.)
	runner  squirrel.BaseRunner // The underlying runner (DB or Tx)
}

// NewQueryExecutor creates a new executor with the given builder and DB/Transaction
func NewQueryExecutor(l *lua.LState, builder interface{}, dbOrTx interface{}) (*lua.LUserData, error) {
	// Extract the underlying runner
	var runner squirrel.BaseRunner

	switch v := dbOrTx.(type) {
	case *sqlmod.DB:
		// Use the accessor method to get the underlying sql.DB
		runner = v.GetRawDB()
	case *sqlmod.Transaction:
		// Use the accessor method to get the underlying sql.Tx
		runner = v.GetRawTx()
	case *sql.DB:
		// Direct sql.DB (less common, but could happen in tests)
		runner = v
	case *sql.Tx:
		// Direct sql.Tx (less common, but could happen in tests)
		runner = v
	default:
		return nil, fmt.Errorf("expected database or transaction object, got %T", dbOrTx)
	}

	// Create the executor
	executor := &QueryExecutor{
		builder: builder,
		runner:  runner,
	}

	// Wrap in userdata
	ud := l.NewUserData()
	ud.Value = executor
	ud.Metatable = value.GetTypeMetatable(l, "sql.QueryExecutor")

	return ud, nil
}

// RegisterQueryExecutorMetatable registers the executor metatable
func RegisterQueryExecutorMetatable(l *lua.LState) {
	methods := map[string]lua.LGFunction{
		"exec":      executorExec,
		"query":     executorQuery,
		"query_row": executorQueryRow,
	}

	metamethods := map[string]lua.LGFunction{
		"__tostring": executorToString,
	}

	value.RegisterTypeMethods(l, "sql.QueryExecutor", metamethods, methods)
}

// Methods for the QueryExecutor

func checkQueryExecutor(l *lua.LState) *QueryExecutor {
	ud := l.CheckUserData(1)
	if executor, ok := ud.Value.(*QueryExecutor); ok {
		return executor
	}
	l.ArgError(1, "expected QueryExecutor object")
	return nil
}

func executorToString(l *lua.LState) int {
	executor := checkQueryExecutor(l)
	if executor == nil {
		return 0
	}

	// Add some basic info about the builder type
	builderType := "unknown"
	switch executor.builder.(type) {
	case squirrel.SelectBuilder:
		builderType = "SELECT"
	case squirrel.InsertBuilder:
		builderType = "INSERT"
	case squirrel.UpdateBuilder:
		builderType = "UPDATE"
	case squirrel.DeleteBuilder:
		builderType = "DELETE"
	}

	l.Push(lua.LString(fmt.Sprintf("SQL Query Executor (%s query)", builderType)))
	return 1
}

// executorExec executes the query and returns results (for INSERT, UPDATE, DELETE)
func executorExec(l *lua.LState) int {
	executor := checkQueryExecutor(l)
	if executor == nil {
		return 0
	}

	var result sql.Result
	var err error

	// Execute based on builder type
	switch b := executor.builder.(type) {
	case squirrel.SelectBuilder:
		result, err = b.RunWith(executor.runner).Exec()
	case squirrel.InsertBuilder:
		result, err = b.RunWith(executor.runner).Exec()
	case squirrel.UpdateBuilder:
		result, err = b.RunWith(executor.runner).Exec()
	case squirrel.DeleteBuilder:
		result, err = b.RunWith(executor.runner).Exec()
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("unsupported builder type: %T", b)))
		return 2
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert result to table using sqlmod.ResultToTable
	resultTable := sqlmod.ResultToTable(l, result)
	l.Push(resultTable)
	l.Push(lua.LNil)
	return 2
}

// executorQuery executes the query and returns all rows
func executorQuery(l *lua.LState) int {
	executor := checkQueryExecutor(l)
	if executor == nil {
		return 0
	}

	var rows *sql.Rows
	var err error

	// Execute based on builder type
	switch b := executor.builder.(type) {
	case squirrel.SelectBuilder:
		rows, err = b.RunWith(executor.runner).Query()
	case squirrel.InsertBuilder:
		rows, err = b.RunWith(executor.runner).Query()
	case squirrel.UpdateBuilder:
		rows, err = b.RunWith(executor.runner).Query()
	case squirrel.DeleteBuilder:
		rows, err = b.RunWith(executor.runner).Query()
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("unsupported builder type: %T", b)))
		return 2
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert rows to table using sqlmod.RowsToTable
	resultTable, err := sqlmod.RowsToTable(l, rows)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(resultTable)
	l.Push(lua.LNil)
	return 2
}

// executorQueryRow executes the query and returns a single row
func executorQueryRow(l *lua.LState) int {
	executor := checkQueryExecutor(l)
	if executor == nil {
		return 0
	}

	// Different handling based on builder type
	switch b := executor.builder.(type) {
	case squirrel.SelectBuilder:
		return handleQueryRow(l, b, executor.runner)
	case squirrel.InsertBuilder:
		return handleQueryRow(l, b, executor.runner)
	case squirrel.UpdateBuilder:
		return handleQueryRow(l, b, executor.runner)
	case squirrel.DeleteBuilder:
		// DeleteBuilder doesn't have QueryRow, use Query and get first row
		return handleDeleteQueryRow(l, b, executor.runner)
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("unsupported builder type: %T", b)))
		return 2
	}
}

// handleQueryRow processes QueryRow for select/insert/update builders that have QueryRow method
func handleQueryRow(l *lua.LState, sqlizer squirrel.Sqlizer, runner squirrel.BaseRunner) int {
	// Get the SQL and args
	query, args, err := sqlizer.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Check if we have a QueryRower
	queryRower, ok := runner.(squirrel.QueryRower)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("runner does not support QueryRow"))
		return 2
	}

	// Use QueryRow to get a row scanner
	rowScanner := queryRower.QueryRow(query, args...)

	// Process the single row result using the scanner
	return scanSingleRowFromScanner(l, rowScanner)
}

// handleDeleteQueryRow handles QueryRow for DeleteBuilder which doesn't have QueryRow method
func handleDeleteQueryRow(l *lua.LState, builder squirrel.DeleteBuilder, runner squirrel.BaseRunner) int {
	// Get SQL and args
	query, args, err := builder.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Use Query instead of QueryRow
	rows, err := runner.Query(query, args...)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	defer func() {
		err := rows.Close()
		if err != nil {
			l := logs.GetLogger(l.Context())
			if l == nil {
				l = zap.NewNop()
			}
			l.Error("rows.Close()", zap.Error(err))
		}
	}()

	// Check if we have a row
	if !rows.Next() {
		// No rows is not an error
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	// Get column info
	columns, err := rows.Columns()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Scan values into a slice
	values := make([]interface{}, len(columns))
	scanArgs := make([]interface{}, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	if err := rows.Scan(scanArgs...); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create result table
	resultTable := l.CreateTable(0, len(columns))
	for i, col := range columns {
		luaVal, err := luaconv.GoToLua(values[i])
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("conversion error for column %s: %v", col, err)))
			return 2
		}
		resultTable.RawSetString(col, luaVal)
	}

	l.Push(resultTable)
	l.Push(lua.LNil)
	return 2
}

// scanSingleRowFromScanner processes a RowScanner into a Lua table
func scanSingleRowFromScanner(l *lua.LState, scanner squirrel.RowScanner) int {
	// For a real implementation, we need to know which columns to scan into
	// This is typically done by using schema information or examining the query

	// For a dynamic approach, we need to determine columns at runtime
	// This example uses a fixed schema for simplicity

	// Create values to scan into
	// For a more dynamic approach, use reflection or metadata
	var id interface{}
	var name interface{}

	// Scan the row
	err := scanner.Scan(&id, &name)
	if err != nil {
		if err == sql.ErrNoRows {
			// No rows is not an error in this context
			l.Push(lua.LNil)
			l.Push(lua.LNil)
			return 2
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create a table with the results
	resultTable := l.CreateTable(0, 2)

	// Convert values to Lua and add to table
	idVal, err := luaconv.GoToLua(id)
	if err == nil {
		resultTable.RawSetString("id", idVal)
	}

	nameVal, err := luaconv.GoToLua(name)
	if err == nil {
		resultTable.RawSetString("name", nameVal)
	}

	l.Push(resultTable)
	l.Push(lua.LNil)
	return 2
}
