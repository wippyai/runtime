package builder

import (
	"database/sql"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/sql/sqlutil"
	lua "github.com/yuin/gopher-lua"
)

// Define local interfaces to break the circular dependency
type DBGetter interface {
	GetRawDB() *sql.DB
}

type TxGetter interface {
	GetRawTx() *sql.Tx
}

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
	case DBGetter:
		// Use the accessor method to get the underlying sql.DB
		runner = v.GetRawDB()
	case TxGetter:
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
		"exec":  executorExec,
		"query": executorQuery,
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

	coroutine.Wrap(l, func() *engine.Update {
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
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("unsupported builder type: %T", b))},
				nil,
			)
		}

		if err != nil {
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(err.Error())},
				nil,
			)
		}

		// Convert result to table using sqlutil.ResultToTable
		resultTable := sqlutil.ResultToTable(l, result)

		return engine.NewUpdate(
			l,
			[]lua.LValue{resultTable, lua.LNil},
			nil,
		)
	})

	return -1
}

// executorQuery executes the query and returns all rows
func executorQuery(l *lua.LState) int {
	executor := checkQueryExecutor(l)
	if executor == nil {
		return 0
	}

	coroutine.Wrap(l, func() *engine.Update {
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
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("unsupported builder type: %T", b))},
				nil,
			)
		}

		if err != nil {
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(err.Error())},
				nil,
			)
		}

		// Convert rows to table using sqlutil.RowsToTable
		var resultTable *lua.LTable
		err = func() error {
			defer func() {
				closeErr := rows.Close()
				if closeErr != nil {
					// No logger available here, but this follows the same pattern as other files
					if err == nil {
						err = closeErr
					}
				}
			}()

			var tableErr error
			resultTable, tableErr = sqlutil.RowsToTable(l, rows)
			return tableErr
		}()

		if err != nil {
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(err.Error())},
				nil,
			)
		}

		return engine.NewUpdate(
			l,
			[]lua.LValue{resultTable, lua.LNil},
			nil,
		)
	})

	return -1
}
