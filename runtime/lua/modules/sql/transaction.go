// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

type Transaction struct {
	tx            *sql.Tx
	db            *DB
	cancelCleanup func()
	mu            sync.Mutex
	active        bool
}

func (t *Transaction) GetRawTx() *sql.Tx {
	return t.tx
}

func (t *Transaction) GetDBType() string {
	return t.db.GetDBType()
}

func NewTransaction(ctx context.Context, tx *sql.Tx, db *DB) *Transaction {
	txWrapper := &Transaction{
		tx:     tx,
		db:     db,
		active: true,
	}

	store := resource.GetStore(ctx)
	if store != nil {
		txWrapper.cancelCleanup = store.AddCleanup(func() error {
			txWrapper.mu.Lock()
			defer txWrapper.mu.Unlock()
			if txWrapper.active {
				txWrapper.active = false
				return tx.Rollback()
			}
			return nil
		})
	}

	return txWrapper
}

func NewTransactionUserData(l *lua.LState, tx *Transaction) *lua.LUserData {
	return value.NewTypedUserData(l, tx, transactionTypeName)
}

var transactionMethods = map[string]lua.LGoFunc{
	"db_type":     txDBType,
	"query":       txQuery,
	"execute":     txExecute,
	"prepare":     txPrepare,
	"commit":      txCommit,
	"rollback":    txRollback,
	"savepoint":   txSavepoint,
	"rollback_to": txRollbackTo,
	"release":     txRelease,
}

func checkTransaction(l *lua.LState) *Transaction {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Transaction); ok {
		return v
	}
	l.ArgError(1, "transaction expected")
	return nil
}

func txDBType(l *lua.LState) int {
	tx := checkTransaction(l)
	if tx == nil {
		return 0
	}
	tx.mu.Lock()
	if !tx.active {
		tx.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "transaction is not active").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	tx.mu.Unlock()

	dbType := mapDBTypeFromResourceKind(tx.GetDBType())
	l.Push(lua.LString(dbType))
	l.Push(lua.LNil)
	return 2
}

func txQuery(l *lua.LState) int {
	tx := checkTransaction(l)
	if tx == nil {
		return 0
	}
	tx.mu.Lock()
	if !tx.active {
		tx.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "transaction is not active").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	tx.mu.Unlock()

	query := normalizePlaceholders(tx.GetDBType(), l.CheckString(2))
	params, err := checkParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "check params").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireTxQueryYield()
	yield.Tx = tx.tx
	yield.Query = query
	yield.Params = params
	l.Push(yield)
	return -1
}

func txExecute(l *lua.LState) int {
	tx := checkTransaction(l)
	if tx == nil {
		return 0
	}
	tx.mu.Lock()
	if !tx.active {
		tx.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "transaction is not active").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	tx.mu.Unlock()

	query := normalizePlaceholders(tx.GetDBType(), l.CheckString(2))
	params, err := checkParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "check params").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireTxExecuteYield()
	yield.Tx = tx.tx
	yield.Query = query
	yield.Params = params
	l.Push(yield)
	return -1
}

func txPrepare(l *lua.LState) int {
	tx := checkTransaction(l)
	if tx == nil {
		return 0
	}
	ctx := l.Context()

	tx.mu.Lock()
	if !tx.active {
		tx.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "transaction is not active").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	tx.mu.Unlock()

	query := normalizePlaceholders(tx.GetDBType(), l.CheckString(2))

	yield := AcquireTxPrepareYield()
	yield.Tx = tx.tx
	yield.Query = query
	yield.WrapStmt = func(stmt *sql.Stmt) lua.LValue {
		return NewStatementUserData(l, NewStatement(ctx, stmt, tx.db))
	}

	l.Push(yield)
	return -1
}

func txCommit(l *lua.LState) int {
	tx := checkTransaction(l)
	if tx == nil {
		return 0
	}
	tx.mu.Lock()
	if !tx.active {
		tx.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "transaction is not active").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	tx.mu.Unlock()

	yield := AcquireTxCommitYield()
	yield.Tx = tx.tx
	yield.OnComplete = func() {
		tx.mu.Lock()
		tx.active = false
		cancel := tx.cancelCleanup
		tx.cancelCleanup = nil
		tx.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	l.Push(yield)
	return -1
}

func txRollback(l *lua.LState) int {
	tx := checkTransaction(l)
	if tx == nil {
		return 0
	}
	tx.mu.Lock()
	if !tx.active {
		tx.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "transaction is not active").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	tx.mu.Unlock()

	yield := AcquireTxRollbackYield()
	yield.Tx = tx.tx
	yield.OnComplete = func() {
		tx.mu.Lock()
		tx.active = false
		cancel := tx.cancelCleanup
		tx.cancelCleanup = nil
		tx.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	l.Push(yield)
	return -1
}

func txSavepoint(l *lua.LState) int {
	tx := checkTransaction(l)
	if tx == nil {
		return 0
	}
	tx.mu.Lock()
	if !tx.active {
		tx.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "transaction is not active").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	tx.mu.Unlock()

	name := l.CheckString(2)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "savepoint name is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	if !isValidSavepointName(name) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "savepoint name can only contain alphanumeric characters and underscores").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	query := fmt.Sprintf("SAVEPOINT %s", name)
	yield := AcquireTxSavepointYield()
	yield.Tx = tx.tx
	yield.Query = query
	l.Push(yield)
	return -1
}

func txRollbackTo(l *lua.LState) int {
	tx := checkTransaction(l)
	if tx == nil {
		return 0
	}
	tx.mu.Lock()
	if !tx.active {
		tx.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "transaction is not active").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	tx.mu.Unlock()

	name := l.CheckString(2)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "savepoint name is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	if !isValidSavepointName(name) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "savepoint name can only contain alphanumeric characters and underscores").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	query := fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", name)
	yield := AcquireTxSavepointYield()
	yield.Tx = tx.tx
	yield.Query = query
	l.Push(yield)
	return -1
}

func txRelease(l *lua.LState) int {
	tx := checkTransaction(l)
	if tx == nil {
		return 0
	}
	tx.mu.Lock()
	if !tx.active {
		tx.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "transaction is not active").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	tx.mu.Unlock()

	name := l.CheckString(2)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "savepoint name is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	if !isValidSavepointName(name) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "savepoint name can only contain alphanumeric characters and underscores").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	query := fmt.Sprintf("RELEASE SAVEPOINT %s", name)
	yield := AcquireTxSavepointYield()
	yield.Tx = tx.tx
	yield.Query = query
	l.Push(yield)
	return -1
}

func isValidSavepointName(name string) bool {
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
