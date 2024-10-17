package sql

import (
	"context"
	"database/sql"
	"time"

	"git.spiralscout.com/estimation-engine/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
)

// execute wraps the DB.execute method.
func execute(l *lua.LState) int {
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

	return db.execute(l)
}

// execute executes a SQL statement.
func (db *DB) execute(l *lua.LState) int {
	db.log.Debug("calling DB.execute")

	// we expect 2 arguments, string and table. First is the query, second is the arguments
	numArgs := l.GetTop()
	if numArgs < 1 {
		db.log.Error("expected at least 1 argument")
		l.Push(lua.LNil)
		l.Push(lua.LString("expected at least 1 argument"))
		return 2
	}

	// 1st argument is the query (0 arg is self)
	qs := l.Get(1)
	if qs.Type() != lua.LTString {
		db.log.Error("expected string as first argument")
		l.Push(lua.LNil)
		l.Push(lua.LString("expected string as first argument"))
		return 2
	}

	// 2nd argument is the table
	argsT := l.Get(2)
	if argsT.Type() != lua.LTTable && argsT.Type() != lua.LTNil {
		db.log.Error("expected table as second argument")
		l.Push(lua.LNil)
		l.Push(lua.LString("expected table as second argument"))
		return 2
	}

	// Convert Lua table to any slice
	args := make([]any, 0)
	if argsT.Type() != lua.LTNil {
		args = engine.TableToAnySlice(argsT, db.log)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var res sql.Result
	var err error
	if db.transaction != nil {
		db.log.Debug("transaction is active, executing on a transaction")
		res, err = db.transaction.ExecContext(ctx, qs.String(), args...)
		if err != nil {
			db.log.Error("failed to execute query on a transaction, should be rolled back", zap.Error(err))
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	} else {
		// execute on a regular connection
		res, err = db.conn.ExecContext(ctx, qs.String(), args...)
		if err != nil {
			db.log.Error("failed to execute query", zap.Error(err))
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
