package sql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ponyruntime/pony/runtime/lua/engine/value"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Transaction represents a database transaction for Lua
type Transaction struct {
	tx        *sql.Tx
	db        *DB
	log       *zap.Logger
	active    bool
	onRelease context.CancelFunc
}

// NewTransaction creates a new Transaction with UoW integration
func NewTransaction(uw engine.UnitOfWork, tx *sql.Tx, db *DB, log *zap.Logger) *Transaction {
	txWrapper := &Transaction{
		tx:     tx,
		db:     db,
		log:    log,
		active: true,
	}

	// Register unconditional cleanup in UoW - directly pass tx.Rollback
	txWrapper.onRelease = uw.AddCleanup(tx.Rollback)

	return txWrapper
}

// WrapTransaction wraps a Transaction as Lua userdata
func WrapTransaction(l *lua.LState, tx *Transaction) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = tx
	ud.Metatable = value.GetTypeMetatable(l, "sql.Transaction")

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
	methods := map[string]lua.LGFunction{
		// Standard transaction methods
		"query":    txQuery,
		"execute":  txExecute,
		"prepare":  txPrepare,
		"commit":   txCommit,
		"rollback": txRollback,

		// Savepoint methods
		"savepoint":   txSavepoint,
		"rollback_to": txRollbackTo,
		"release":     txReleaseSavepoint,
	}

	value.RegisterMethods(l, "sql.Transaction", methods)
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

	coroutine.Wrap(l, func() *engine.Update {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewUpdate(nil, nil, fmt.Errorf("transaction is not active"))
		}

		var rows *sql.Rows
		var err error

		// Serve query with appropriate parameter style
		switch p := params.(type) {
		case nil:
			rows, err = tx.tx.Query(query)
		case []interface{}:
			rows, err = tx.tx.Query(query, p...)
		default:
			return engine.NewUpdate(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		var resultTable *lua.LTable
		// Use a named return parameter to capture errors from both rowsToTable and rows.close
		err = func() error {
			defer func() {
				closeErr := rows.Close()
				if closeErr != nil {
					tx.log.Error("failed to close rows", zap.Error(closeErr))
					// If we don't already have an error, use the close error
					if err == nil {
						err = closeErr
					}
				}
			}()

			// Convert rows to Lua table
			var tableErr error
			resultTable, tableErr = rowsToTable(l, rows)
			return tableErr
		}()

		if err != nil {
			return engine.NewUpdate(nil, nil, err)
		}

		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
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

	coroutine.Wrap(l, func() *engine.Update {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewUpdate(nil, nil, fmt.Errorf("transaction is not active"))
		}

		var result sql.Result
		var err error

		// Serve with appropriate parameter style
		switch p := params.(type) {
		case nil:
			result, err = tx.tx.Exec(query)
		case []interface{}:
			result, err = tx.tx.Exec(query, p...)
		default:
			return engine.NewUpdate(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Convert result to Lua table
		resultTable := resultToTable(l, result)

		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
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

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work found in context")
		return 0
	}

	// Get query
	query := l.CheckString(2)

	coroutine.Wrap(l, func() *engine.Update {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewUpdate(nil, nil, fmt.Errorf("transaction is not active"))
		}

		// Prepare statement
		stmt, err := tx.tx.Prepare(query)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Create statement wrapper using the constructor
		stmtObj := NewStatement(uw, stmt, tx.db, tx.log)

		// Create userdata
		ud := WrapStatement(l, stmtObj)

		return engine.NewUpdate(nil, []lua.LValue{ud, lua.LNil}, nil)
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

	coroutine.Wrap(l, func() *engine.Update {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewUpdate(nil, nil, fmt.Errorf("transaction is not active"))
		}

		// Commit transaction
		if err := tx.tx.Commit(); err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Mark as inactive
		tx.active = false

		// Cancel the cleanup function in UoW (don't execute it, just remove it)
		if tx.onRelease != nil {
			tx.onRelease()
			tx.onRelease = nil
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LTrue, lua.LNil}, nil)
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

	coroutine.Wrap(l, func() *engine.Update {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewUpdate(nil, nil, fmt.Errorf("transaction is not active"))
		}

		// Rollback transaction explicitly
		if err := tx.tx.Rollback(); err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Mark as inactive after successful rollback
		tx.active = false

		// Cancel the cleanup function in UoW (don't execute it, just remove it)
		if tx.onRelease != nil {
			tx.onRelease()
			tx.onRelease = nil
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LTrue, lua.LNil}, nil)
	})

	return -1 // Yield
}

// txSavepoint creates a savepoint in the transaction
func txSavepoint(l *lua.LState) int {
	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	// Get savepoint name
	name := l.CheckString(2)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("savepoint name is required"))
		return 2
	}

	// Sanitize the savepoint name to prevent SQL injection
	// Only allow alphanumeric and underscore characters
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			l.Push(lua.LNil)
			l.Push(lua.LString("savepoint name can only contain alphanumeric characters and underscores"))
			return 2
		}
	}

	coroutine.Wrap(l, func() *engine.Update {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewUpdate(nil, nil, fmt.Errorf("transaction is not active"))
		}

		// Create savepoint
		query := fmt.Sprintf("SAVEPOINT %s", name)
		_, err := tx.tx.Exec(query)
		if err != nil {
			return engine.NewUpdate(nil, nil, fmt.Errorf("failed to create savepoint: %w", err))
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LTrue, lua.LNil}, nil)
	})

	return -1 // Yield
}

// txRollbackTo rolls back to a savepoint in the transaction
func txRollbackTo(l *lua.LState) int {
	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	// Get savepoint name
	name := l.CheckString(2)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("savepoint name is required"))
		return 2
	}

	// Sanitize the savepoint name
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			l.Push(lua.LNil)
			l.Push(lua.LString("savepoint name can only contain alphanumeric characters and underscores"))
			return 2
		}
	}

	coroutine.Wrap(l, func() *engine.Update {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewUpdate(nil, nil, fmt.Errorf("transaction is not active"))
		}

		// Roll back to savepoint
		query := fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", name)
		_, err := tx.tx.Exec(query)
		if err != nil {
			return engine.NewUpdate(nil, nil, fmt.Errorf("failed to rollback to savepoint: %w", err))
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LTrue, lua.LNil}, nil)
	})

	return -1 // Yield
}

// txReleaseSavepoint releases a savepoint in the transaction
func txReleaseSavepoint(l *lua.LState) int {
	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	// Get savepoint name
	name := l.CheckString(2)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("savepoint name is required"))
		return 2
	}

	// Sanitize the savepoint name
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			l.Push(lua.LNil)
			l.Push(lua.LString("savepoint name can only contain alphanumeric characters and underscores"))
			return 2
		}
	}

	coroutine.Wrap(l, func() *engine.Update {
		// Check if transaction is still active
		if !tx.active {
			return engine.NewUpdate(nil, nil, fmt.Errorf("transaction is not active"))
		}

		// Release savepoint
		query := fmt.Sprintf("RELEASE SAVEPOINT %s", name)
		_, err := tx.tx.Exec(query)
		if err != nil {
			return engine.NewUpdate(nil, nil, fmt.Errorf("failed to release savepoint: %w", err))
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LTrue, lua.LNil}, nil)
	})

	return -1 // Yield
}
