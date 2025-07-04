package sql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ponyruntime/pony/runtime/lua/modules/sql/sqlutil"

	"github.com/ponyruntime/pony/runtime/lua/engine/value"

	"github.com/ponyruntime/pony/runtime/lua/engine"
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

// In your sql package, add to Transaction type:
// GetRawTx exposes the underlying sql.Tx for external components like QueryBuilder
func (t *Transaction) GetRawTx() *sql.Tx {
	return t.tx
}

func (t *Transaction) GetDBType() string {
	return t.db.GetDBType()
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
func registerTransaction(l *lua.LState, _ *zap.Logger) {
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
	ctx := l.Context()

	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	// Get query and parameters
	query := l.CheckString(2)
	params, err := sqlutil.CheckParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Check if transaction is still active
	if !tx.active {
		l.Push(lua.LNil)
		l.Push(lua.LString("transaction is not active"))
		return 2
	}

	var rows *sql.Rows

	// Serve query with appropriate parameter style
	switch p := params.(type) {
	case nil:
		rows, err = tx.tx.QueryContext(ctx, query)
	case []interface{}:
		rows, err = tx.tx.QueryContext(ctx, query, p...)
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("unsupported parameter type: %T", params)))
		return 2
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	var resultTable *lua.LTable
	// Use a named return parameter to capture errors from both RowsToTable and rows.close
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
		resultTable, tableErr = sqlutil.RowsToTable(l, rows)
		return tableErr
	}()

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(resultTable)
	l.Push(lua.LNil)
	return 2
}

// txExecute executes a statement within a transaction that doesn't return rows
func txExecute(l *lua.LState) int {
	ctx := l.Context()

	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	// Get query and parameters
	query := l.CheckString(2)
	params, err := sqlutil.CheckParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Check if transaction is still active
	if !tx.active {
		l.Push(lua.LNil)
		l.Push(lua.LString("transaction is not active"))
		return 2
	}

	var result sql.Result

	// Serve with appropriate parameter style
	switch p := params.(type) {
	case nil:
		result, err = tx.tx.ExecContext(ctx, query)
	case []interface{}:
		result, err = tx.tx.ExecContext(ctx, query, p...)
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("unsupported parameter type: %T", params)))
		return 2
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert result to Lua table
	resultTable := sqlutil.ResultToTable(l, result)

	l.Push(resultTable)
	l.Push(lua.LNil)
	return 2
}

// txPrepare prepares a statement within a transaction for repeated execution
func txPrepare(l *lua.LState) int {
	ctx := l.Context()

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

	// Check if transaction is still active
	if !tx.active {
		l.Push(lua.LNil)
		l.Push(lua.LString("transaction is not active"))
		return 2
	}

	// Prepare statement
	stmt, err := tx.tx.PrepareContext(ctx, query)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create statement wrapper using the constructor
	stmtObj := NewStatement(uw, stmt, tx.db, tx.log)

	// Create userdata
	ud := WrapStatement(l, stmtObj)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// txCommit commits the transaction
func txCommit(l *lua.LState) int {
	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	// Check if transaction is still active
	if !tx.active {
		l.Push(lua.LNil)
		l.Push(lua.LString("transaction is not active"))
		return 2
	}

	// Commit transaction
	if err := tx.tx.Commit(); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Mark as inactive
	tx.active = false

	// Cancel the cleanup function in UoW (don't execute it, just remove it)
	if tx.onRelease != nil {
		tx.onRelease()
		tx.onRelease = nil
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// txRollback rolls back the transaction
func txRollback(l *lua.LState) int {
	// Check and get transaction
	tx := CheckTransaction(l)
	if tx == nil {
		return 0
	}

	// Check if transaction is still active
	if !tx.active {
		l.Push(lua.LNil)
		l.Push(lua.LString("transaction is not active"))
		return 2
	}

	// Rollback transaction explicitly
	if err := tx.tx.Rollback(); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Mark as inactive after successful rollback
	tx.active = false

	// Cancel the cleanup function in UoW (don't execute it, just remove it)
	if tx.onRelease != nil {
		tx.onRelease()
		tx.onRelease = nil
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// txSavepoint creates a savepoint in the transaction
func txSavepoint(l *lua.LState) int {
	ctx := l.Context()

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
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			l.Push(lua.LNil)
			l.Push(lua.LString("savepoint name can only contain alphanumeric characters and underscores"))
			return 2
		}
	}

	// Check if transaction is still active
	if !tx.active {
		l.Push(lua.LNil)
		l.Push(lua.LString("transaction is not active"))
		return 2
	}

	// Create savepoint
	query := fmt.Sprintf("SAVEPOINT %s", name)
	_, err := tx.tx.ExecContext(ctx, query)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create savepoint: %v", err)))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// txRollbackTo rolls back to a savepoint in the transaction
func txRollbackTo(l *lua.LState) int {
	ctx := l.Context()

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
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			l.Push(lua.LNil)
			l.Push(lua.LString("savepoint name can only contain alphanumeric characters and underscores"))
			return 2
		}
	}

	// Check if transaction is still active
	if !tx.active {
		l.Push(lua.LNil)
		l.Push(lua.LString("transaction is not active"))
		return 2
	}

	// Roll back to savepoint
	query := fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", name)
	_, err := tx.tx.ExecContext(ctx, query)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to rollback to savepoint: %v", err)))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// txReleaseSavepoint releases a savepoint in the transaction
func txReleaseSavepoint(l *lua.LState) int {
	ctx := l.Context()

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
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			l.Push(lua.LNil)
			l.Push(lua.LString("savepoint name can only contain alphanumeric characters and underscores"))
			return 2
		}
	}

	// Check if transaction is still active
	if !tx.active {
		l.Push(lua.LNil)
		l.Push(lua.LString("transaction is not active"))
		return 2
	}

	// Release savepoint
	query := fmt.Sprintf("RELEASE SAVEPOINT %s", name)
	_, err := tx.tx.ExecContext(ctx, query)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to release savepoint: %v", err)))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
