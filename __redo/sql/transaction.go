package sql

import (
	"context"
	"database/sql"

	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// beginTransaction wraps the DB.beginTransaction method.
func beginTransaction(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if ud == nil {
		l.ArgError(1, "expected userdata for DB")
		return 0
	}

	db, ok := ud.Value.(*DB)
	if !ok {
		l.ArgError(1, "invalid userdata type for DB")
		return 0
	}

	// remove self from args
	l.Remove(1)

	return db.beginTransaction(l)
}

// commit wraps the DB.commit method.
func commit(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if ud == nil {
		l.ArgError(1, "expected userdata for DB")
		return 0
	}

	db, ok := ud.Value.(*DB)
	if !ok {
		l.ArgError(1, "invalid userdata type for DB")
		return 0
	}

	// remove self from args
	l.Remove(1)

	return db.commit(l)
}

// rollback wraps the DB.rollback method.
func rollback(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if ud == nil {
		l.ArgError(1, "expected userdata for DB")
		return 0
	}

	db, ok := ud.Value.(*DB)
	if !ok {
		l.ArgError(1, "invalid userdata type for DB")
		return 0
	}

	// remove self from args
	l.Remove(1)

	return db.rollback(l)
}

// beginTransaction starts a new transaction.
func (db *DB) beginTransaction(l *lua.LState) int {
	db.log.Debug("calling DB.beginTransaction")

	if db.transaction != nil {
		// rollback the current transaction
		_ = db.transaction.Rollback()
		db.log.Error("transaction already exists")
		l.Push(lua.LString("transaction already exists, please commit or rollback the current transaction"))
		return 1
	}

	txx, err := db.conn.BeginTx(context.Background(), &sql.TxOptions{
		ReadOnly: false,
	})
	if err != nil {
		db.log.Error("failed to begin transaction", zap.Error(err))
		l.Push(lua.LString(err.Error()))
		return 1
	}

	// save the transaction
	db.transaction = txx
	l.Push(lua.LNil)

	return 1
}

// commit commits the current transaction.
func (db *DB) commit(l *lua.LState) int {
	db.log.Debug("calling DB.commit")

	if db.transaction == nil {
		db.log.Error("transaction is not open or already exhausted")
		l.Push(lua.LString("transaction is not open or already exhausted"))
		return 1
	}

	err := db.transaction.Commit()
	if err != nil {
		db.log.Error("failed to commit transaction", zap.Error(err))
		l.Push(lua.LString(err.Error()))
		return 1
	}

	// clear the transaction
	db.transaction = nil
	l.Push(lua.LNil)

	return 1
}

// rollback rolls back the current transaction.
func (db *DB) rollback(l *lua.LState) int {
	db.log.Debug("calling DB.rollback")

	if db.transaction == nil {
		db.log.Error("transaction is not open or already exhausted")
		l.Push(lua.LString("transaction is not open or already exhausted"))
		return 1
	}

	err := db.transaction.Rollback()
	if err != nil {
		db.log.Error("failed to rollback the transaction", zap.Error(err))
		l.Push(lua.LString(err.Error()))
		return 1
	}

	// clear the transaction
	db.transaction = nil
	l.Push(lua.LNil)

	return 1
}

// close closes the database connection.
func (db *DB) close(l *lua.LState) int {
	db.log.Debug("calling DB.close")

	if db.conn != nil {
		err := db.conn.Close()
		if err != nil {
			db.log.Error("failed to close connection", zap.Error(err))
			l.Push(lua.LString(err.Error()))
			return 1
		}
		db.conn = nil
	}

	l.Push(lua.LNil)
	return 1
}
