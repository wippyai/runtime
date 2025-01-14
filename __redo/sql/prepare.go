package sql

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// prepare wraps the DB.prepare method.
func prepare(l *lua.LState) int {
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

	return db.prepare(l)
}

// executePrepared wraps the DB.executePrepared method.
func executePrepared(l *lua.LState) int {
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

	return db.executePrepared(l)
}

// prepare prepares a SQL statement.
func (db *DB) prepare(l *lua.LState) int {
	db.log.Debug("calling DB.prepare")

	// we expect 1 argument, string with the prepared statement
	numArgs := l.GetTop()
	if numArgs != 1 {
		db.log.Error("expected 1 argument: prepared statement")
		l.Push(lua.LNil)
		l.Push(lua.LString("expected 1 argument: prepared statement"))
		return 2
	}

	// 1st argument is the prepared statement
	qs := l.Get(1)
	if qs.Type() != lua.LTString {
		db.log.Error("expected string as first argument")
		l.Push(lua.LNil)
		l.Push(lua.LString("expected string as first argument"))
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var stmt *sql.Stmt
	var err error
	// we can execute a prepared statement on a transaction or on a regular connection
	if db.transaction != nil {
		db.log.Debug("transaction is active, preparing on a transaction")
		stmt, err = db.transaction.PrepareContext(ctx, qs.String())
		if err != nil {
			db.log.Error("failed to prepare query on a transaction", zap.Error(err))
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	} else {
		stmt, err = db.conn.PrepareContext(ctx, qs.String())
		if err != nil {
			db.log.Error("failed to prepare query", zap.Error(err))
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	}

	id := uuid.NewString()
	db.prepstmap[id] = stmt

	l.Push(lua.LString(id))
	l.Push(lua.LNil)

	return 2
}

// executePrepared executes a prepared SQL statement.
func (db *DB) executePrepared(l *lua.LState) int {
	db.log.Debug("calling DB.executePrepared")

	if len(db.prepstmap) == 0 {
		db.log.Error("no prepared statements")
		l.Push(lua.LNil)
		l.Push(lua.LString("no prepared statements, call db:prepare first"))
		return 2
	}

	// we expect 2 arguments, string with the Name and table with arguments
	numArgs := l.GetTop()
	if numArgs != 2 {
		db.log.Error("expected 2 arguments")
		l.Push(lua.LNil)
		l.Push(lua.LString("expected 2 arguments"))
		return 2
	}

	// 1st argument is the Name
	id := l.CheckString(1)

	// 2nd argument is the table
	argsT := l.Get(2)
	if argsT.Type() != lua.LTTable {
		db.log.Error("expected table as second argument")
		l.Push(lua.LNil)
		l.Push(lua.LString("expected table as second argument"))
		return 2
	}

	// Convert Lua table to slice
	args := engine.TableToAnySlice(argsT, db.log)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	prepSt, ok := db.prepstmap[id]
	if !ok {
		db.log.Error("prepared statement not found by provided Name")
		l.Push(lua.LNil)
		l.Push(lua.LString("prepared statement not found by provided Name"))
		return 2
	}

	if prepSt == nil {
		db.log.Error("prepared statement is found but it is nil")
		l.Push(lua.LNil)
		l.Push(lua.LString("prepared statement is found but it is nil"))
		return 2
	}

	var res sql.Result
	var err error
	if db.transaction != nil {
		db.log.Debug("transaction is active, executing on a transaction")
		res, err = db.transaction.StmtContext(ctx, prepSt).ExecContext(ctx, args...)
		if err != nil {
			db.log.Error("failed to execute prepared statement on a transaction, should be rolled back", zap.Error(err))
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	} else {
		// execute on a regular connection
		res, err = prepSt.ExecContext(ctx, args...)
		if err != nil {
			db.log.Error("failed to execute prepared statement", zap.Error(err))
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	}

	rs, err := db.wrapResult(l, res)
	if err != nil {
		db.log.Error("failed to wrap result", zap.Error(err))
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(rs)
	l.Push(lua.LNil)

	return 2
}
