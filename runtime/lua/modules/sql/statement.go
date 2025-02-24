package sql

import (
	"database/sql"
	"fmt"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Statement represents a prepared statement for Lua
type Statement struct {
	stmt   *sql.Stmt
	db     *DB
	log    *zap.Logger
	closed bool
}

// WrapStatement wraps a Statement as Lua userdata
func WrapStatement(l *lua.LState, stmt *Statement) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = stmt
	l.SetMetatable(ud, l.GetTypeMetatable("sql.Statement"))
	return ud
}

// CheckStatement checks if the first argument is a Statement and returns it
func CheckStatement(l *lua.LState) *Statement {
	ud := l.CheckUserData(1)
	if stmt, ok := ud.Value.(*Statement); ok {
		return stmt
	}
	l.ArgError(1, "expected statement object")
	return nil
}

// registerStatement registers statement methods
func registerStatement(l *lua.LState, log *zap.Logger) {
	// Register statement metatable
	mt := l.NewTypeMetatable("sql.Statement")
	methods := l.NewTable()

	methods.RawSetString("query", l.NewFunction(stmtQuery))
	methods.RawSetString("execute", l.NewFunction(stmtExecute))
	methods.RawSetString("close", l.NewFunction(stmtClose))

	l.SetField(mt, "__index", methods)
}

// stmtQuery executes a prepared query and returns rows
func stmtQuery(l *lua.LState) int {
	// Check and get statement.
	stmt := CheckStatement(l)
	if stmt == nil {
		return 0
	}

	// Return an error if the statement has been closed.
	if stmt.closed {
		l.Push(lua.LNil)
		l.Push(lua.LString("statement is closed"))
		return 2
	}

	// Get parameters.
	params, err := checkParams(l, 2)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	coroutine.Wrap(l, func() *engine.Result {
		var rows *sql.Rows
		var err error

		// Execute query with appropriate parameter style.
		switch p := params.(type) {
		case nil:
			rows, err = stmt.stmt.Query()
		case []interface{}:
			rows, err = stmt.stmt.Query(p...)
		default:
			return engine.NewResult(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewResult(nil, nil, err)
		}

		var resultTable *lua.LTable
		err = func() error {
			defer func() {
				closeErr := rows.Close()
				if closeErr != nil {
					stmt.log.Error("failed to close rows", zap.Error(closeErr))
					if err == nil {
						err = closeErr
					}
				}
			}()
			var tableErr error
			resultTable, tableErr = rowsToTable(l, rows)
			return tableErr
		}()

		if err != nil {
			return engine.NewResult(nil, nil, err)
		}

		return engine.NewResult(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield.
}

// stmtExecute executes a prepared statement that doesn't return rows
func stmtExecute(l *lua.LState) int {
	// Check and get statement.
	stmt := CheckStatement(l)
	if stmt == nil {
		return 0
	}

	// Return an error if the statement has been closed.
	if stmt.closed {
		l.Push(lua.LNil)
		l.Push(lua.LString("statement is closed"))
		return 2
	}

	// Get parameters.
	params, err := checkParams(l, 2)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	coroutine.Wrap(l, func() *engine.Result {
		var result sql.Result
		var err error

		// Execute with appropriate parameter style.
		switch p := params.(type) {
		case nil:
			result, err = stmt.stmt.Exec()
		case []interface{}:
			result, err = stmt.stmt.Exec(p...)
		default:
			return engine.NewResult(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewResult(nil, nil, err)
		}

		// Convert result to Lua table.
		resultTable := resultToTable(l, result)
		return engine.NewResult(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield.
}

// stmtClose closes a prepared statement
func stmtClose(l *lua.LState) int {
	// Check and get statement.
	stmt := CheckStatement(l)
	if stmt == nil {
		return 0
	}

	// If already closed, return error.
	if stmt.closed {
		l.Push(lua.LNil)
		l.Push(lua.LString("statement is already closed"))
		return 2
	}

	err := stmt.Close()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	stmt.closed = true
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// Close closes the statement directly - for use outside Lua context
func (s *Statement) Close() error {
	s.closed = true
	return s.stmt.Close()
}
