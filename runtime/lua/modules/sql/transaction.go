package sql

import (
	"database/sql"
	"fmt"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/uow"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Transaction represents a database transaction for Lua
type Transaction struct {
	tx     *sql.Tx
	db     *DB
	log    *zap.Logger
	active bool
}

// WrapTransaction wraps a Transaction as Lua userdata
func WrapTransaction(l *lua.LState, tx *Transaction) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = tx
	l.SetMetatable(ud, l.GetTypeMetatable("sql.Transaction"))
	return ud
}

// CheckTransaction checks if the first argument is a Transaction and returns it
func CheckTransaction(l *lua.LState) *Transaction {
	ud := l.CheckUserData(1)
	if tx, ok := ud.Value.(*Transaction); ok {
		return tx
	}
	l.ArgError(1, "expected transaction object")
	return nil
}

// registerTransaction registers transaction methods
func registerTransaction(l *lua.LState, log *zap.Logger) {
	// Register transaction metatable
	mt := l.NewTypeMetatable("sql.Transaction")
	methods := l.NewTable()

	methods.RawSetString("query", l.NewFunction(txQuery))
	methods.RawSetString("execute", l.NewFunction(txExecute))
	methods.RawSetString("prepare", l.NewFunction(txPrepare))
	methods.RawSetString("commit", l.NewFunction(txCommit))
	methods.RawSetString("rollback", l.NewFunction(txRollback))

	l.SetField(mt, "__index", methods)
}

// txQuery executes a query within a transaction and returns rows
func txQuery(l *lua.LState) int {
	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	// Get query and parameters
	query := l.CheckString(2)
	params, err := checkParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	coroutine.Wrap(l, func() *engine.Result {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewResult(nil, nil, fmt.Errorf("transaction is not active"))
		}

		var rows *sql.Rows
		var err error

		// Execute query with appropriate parameter style
		switch p := params.(type) {
		case nil:
			rows, err = tx.tx.Query(query)
		case []interface{}:
			rows, err = tx.tx.Query(query, p...)
		default:
			return engine.NewResult(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewResult(nil, nil, err)
		}
		defer rows.Close()

		// Convert rows to Lua table
		resultTable, err := rowsToTable(l, rows)
		if err != nil {
			return engine.NewResult(nil, nil, err)
		}

		return engine.NewResult(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield
}

// txExecute executes a statement within a transaction that doesn't return rows
func txExecute(l *lua.LState) int {
	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	// Get query and parameters
	query := l.CheckString(2)
	params, err := checkParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	coroutine.Wrap(l, func() *engine.Result {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewResult(nil, nil, fmt.Errorf("transaction is not active"))
		}

		var result sql.Result
		var err error

		// Execute with appropriate parameter style
		switch p := params.(type) {
		case nil:
			result, err = tx.tx.Exec(query)
		case []interface{}:
			result, err = tx.tx.Exec(query, p...)
		default:
			return engine.NewResult(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewResult(nil, nil, err)
		}

		// Convert result to Lua table
		resultTable := resultToTable(l, result)

		return engine.NewResult(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield
}

// txPrepare prepares a statement within a transaction for repeated execution
func txPrepare(l *lua.LState) int {
	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	// Get query
	query := l.CheckString(2)

	coroutine.Wrap(l, func() *engine.Result {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewResult(nil, nil, fmt.Errorf("transaction is not active"))
		}

		// Prepare statement
		stmt, err := tx.tx.Prepare(query)
		if err != nil {
			return engine.NewResult(nil, nil, err)
		}

		// Create statement wrapper
		stmtObj := &Statement{
			stmt: stmt,
			db:   tx.db,
			log:  tx.log,
		}

		// Register for cleanup
		uw := uow.FromContext(l.Context())
		if uw != nil {
			uw.AddCleanup(func() error {
				return stmt.Close()
			})
		}

		// Create userdata
		ud := WrapStatement(l, stmtObj)

		return engine.NewResult(nil, []lua.LValue{ud, lua.LNil}, nil)
	})

	return -1 // Yield
}

// txCommit commits the transaction
func txCommit(l *lua.LState) int {
	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	coroutine.Wrap(l, func() *engine.Result {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewResult(nil, nil, fmt.Errorf("transaction is not active"))
		}

		// Commit transaction
		if err := tx.tx.Commit(); err != nil {
			return engine.NewResult(nil, nil, err)
		}

		// Mark as inactive
		tx.active = false

		return engine.NewResult(nil, []lua.LValue{lua.LTrue, lua.LNil}, nil)
	})

	return -1 // Yield
}

// txRollback rolls back the transaction
func txRollback(l *lua.LState) int {
	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	coroutine.Wrap(l, func() *engine.Result {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewResult(nil, nil, fmt.Errorf("transaction is not active"))
		}

		// Rollback transaction
		if err := tx.tx.Rollback(); err != nil {
			return engine.NewResult(nil, nil, err)
		}

		// Mark as inactive
		tx.active = false

		return engine.NewResult(nil, []lua.LValue{lua.LTrue, lua.LNil}, nil)
	})

	return -1 // Yield
}
