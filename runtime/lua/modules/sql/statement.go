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

// Statement represents a prepared statement for Lua
type Statement struct {
	stmt      *sql.Stmt
	db        *DB
	log       *zap.Logger
	closed    bool
	onRelease context.CancelFunc
}

// NewStatement creates a new Statement with UoW integration
func NewStatement(uw engine.UnitOfWork, stmt *sql.Stmt, db *DB, log *zap.Logger) *Statement {
	stmtWrapper := &Statement{
		stmt:   stmt,
		db:     db,
		log:    log,
		closed: false,
	}

	// Register unconditional cleanup in UoW - directly pass stmt.Close
	stmtWrapper.onRelease = uw.AddCleanup(stmt.Close)

	return stmtWrapper
}

// WrapStatement wraps a Statement as Lua userdata
func WrapStatement(l *lua.LState, stmt *Statement) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = stmt
	ud.Metatable = value.GetTypeMetatable(l, "sql.Statement")

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
	methods := map[string]lua.LGFunction{
		"query":   stmtQuery,
		"execute": stmtExecute,
		"close":   stmtClose,
	}

	value.RegisterMethods(l, "sql.Statement", methods)
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

	coroutine.Wrap(l, func() *engine.Update {
		var rows *sql.Rows
		var err error

		// Start query with appropriate parameter style.
		switch p := params.(type) {
		case nil:
			rows, err = stmt.stmt.Query()
		case []interface{}:
			rows, err = stmt.stmt.Query(p...)
		default:
			return engine.NewUpdate(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewUpdate(nil, nil, err)
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
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
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

	coroutine.Wrap(l, func() *engine.Update {
		var result sql.Result
		var err error

		// Start with appropriate parameter style.
		switch p := params.(type) {
		case nil:
			result, err = stmt.stmt.Exec()
		case []interface{}:
			result, err = stmt.stmt.Exec(p...)
		default:
			return engine.NewUpdate(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Convert result to Lua table.
		resultTable := resultToTable(l, result)
		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
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

	// We need to close explicitly and then cancel the UoW cleanup
	err := stmt.stmt.Close()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Mark as closed after successful close
	stmt.closed = true

	// Cancel the cleanup function in UoW (don't execute it, just remove it)
	if stmt.onRelease != nil {
		stmt.onRelease()
		stmt.onRelease = nil
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// Close closes the statement directly - for use outside Lua context
func (s *Statement) Close() error {
	if s.closed {
		return nil
	}

	// Close the statement directly
	err := s.stmt.Close()
	if err != nil {
		return err
	}

	// Mark as closed after successful close
	s.closed = true

	// Cancel the cleanup function in UoW (don't execute it, just remove it)
	if s.onRelease != nil {
		s.onRelease()
		s.onRelease = nil
	}

	return nil
}
