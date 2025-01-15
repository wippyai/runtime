package sql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// query wraps the DB.query method.
func query(l *lua.LState) int {
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

	return db.query(l)
}

// query executes a SQL query and returns the result set.
func (db *DB) query(l *lua.LState) int {
	db.log.Debug("calling DB.query")

	// we expect 2 arguments, string and table. First is the query, second is the arguments
	numArgs := l.GetTop()
	if numArgs < 1 {
		db.log.Error("expected at least 1 argument")
		l.Push(lua.LNil)
		l.Push(lua.LString("expected at least 1 argument"))
		return 2
	}

	// 1st argument is the query
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

	// Convert Lua table to slice
	args := make([]any, 0)

	if argsT.Type() != lua.LTNil {
		args = engine.TableToAnySlice(argsT, db.log)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var rows *sql.Rows
	var err error
	if db.transaction != nil {
		db.log.Debug("transaction is active, executing on a transaction")
		rows, err = db.transaction.QueryContext(ctx, qs.String(), args...)
		if err != nil {
			db.log.Error("failed to execute query on a transaction, should be rolled back", zap.Error(err))
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to execute query on a transaction, should be rolled back, error: %s", err.Error())))
			return 2
		}
	} else {
		// execute on a regular connection
		rows, err = db.conn.QueryContext(ctx, qs.String(), args...)
		if err != nil {
			db.log.Error("failed to execute query", zap.Error(err))
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	}
	defer func() {
		err := rows.Close()
		if err != nil {
			db.log.Error("failed to close rows", zap.Error(err))
		}
	}()

	columns, err := rows.Columns()
	if err != nil {
		db.log.Error("failed to get columns", zap.Error(err))
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	db.log.Debug("query result", zap.Strings("columns", columns))

	data := make([]any, len(columns))
	dataPtrs := make([]any, len(columns))

	resT := l.NewTable()

	// todo: add method with iteration in future
	for rows.Next() {
		for i := range columns {
			dataPtrs[i] = &data[i]
		}

		err = rows.Scan(dataPtrs...)
		if err != nil {
			db.log.Error("failed to scan row", zap.Error(err))
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}

		tmpt := l.NewTable()
		for i, col := range columns {
			switch v := data[i].(type) {
			case string:
				tmpt.RawSetString(col, lua.LString(v))
			case []byte:
				tmpt.RawSetString(col, lua.LString(v))
			case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
				tmpt.RawSetString(col, lua.LNumber(toInt64(v)))
			case float32, float64:
				tmpt.RawSetString(col, lua.LNumber(toFloat64(v)))
			default:
				tmpt.RawSetString(col, lua.LString(fmt.Sprintf("%v", v)))
			}
		}

		resT.Append(tmpt)
	}

	// push the result table and nil
	l.Push(resT)
	l.Push(lua.LNil)

	return 2
}

// Helper functions for type assertions
func toInt64(v any) int64 {
	switch i := v.(type) {
	case int:
		return int64(i)
	case int8:
		return int64(i)
	case int16:
		return int64(i)
	case int32:
		return int64(i)
	case int64:
		return i
	case uint:
		// possible overflow
		return int64(i) //nolint:gosec
	case uint8:
		return int64(i)
	case uint16:
		return int64(i)
	case uint32:
		return int64(i)
	case uint64:
		// possible overflow
		return int64(i) //nolint:gosec
	default:
		return 0
	}
}

func toFloat64(v interface{}) float64 {
	switch f := v.(type) {
	case float32:
		return float64(f)
	case float64:
		return f
	default:
		return 0.0
	}
}
