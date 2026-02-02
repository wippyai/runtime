package sql

import (
	"context"
	"database/sql"
	"sync"

	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/wippyai/go-lua"
)

type Statement struct {
	stmt          *sql.Stmt
	db            *DB
	cancelCleanup func()
	mu            sync.Mutex
	closed        bool
}

func NewStatement(ctx context.Context, stmt *sql.Stmt, db *DB) *Statement {
	s := &Statement{
		stmt:   stmt,
		db:     db,
		closed: false,
	}

	store := resource.GetStore(ctx)
	if store != nil {
		s.cancelCleanup = store.AddCleanup(func() error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.closed {
				s.closed = true
				return s.stmt.Close()
			}
			return nil
		})
	}

	return s
}

func (s *Statement) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	return s.stmt.Close()
}

func NewStatementUserData(l *lua.LState, stmt *Statement) *lua.LUserData {
	return value.NewTypedUserData(l, stmt, statementTypeName)
}

var statementMethods = map[string]lua.LGoFunc{
	"query":   stmtQuery,
	"execute": stmtExecute,
	"close":   stmtClose,
}

//nolint:unparam // idx kept for API consistency with other check functions
func checkStatement(l *lua.LState, idx int) *Statement {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Statement); ok {
		return v
	}
	l.ArgError(idx, "statement expected")
	return nil
}

func stmtQuery(l *lua.LState) int {
	stmt := checkStatement(l, 1)
	if stmt == nil {
		return 0
	}
	stmt.mu.Lock()
	if stmt.closed {
		stmt.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "statement is closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	stmt.mu.Unlock()

	params, err := checkParams(l, 2)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "check params").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireStmtQueryYield()
	yield.Stmt = stmt.stmt
	yield.Params = params
	l.Push(yield)
	return -1
}

func stmtExecute(l *lua.LState) int {
	stmt := checkStatement(l, 1)
	if stmt == nil {
		return 0
	}
	stmt.mu.Lock()
	if stmt.closed {
		stmt.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "statement is closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	stmt.mu.Unlock()

	params, err := checkParams(l, 2)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "check params").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireStmtExecuteYield()
	yield.Stmt = stmt.stmt
	yield.Params = params
	l.Push(yield)
	return -1
}

func stmtClose(l *lua.LState) int {
	stmt := checkStatement(l, 1)
	if stmt == nil {
		return 0
	}
	stmt.mu.Lock()
	if stmt.closed {
		stmt.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "statement is already closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	stmt.mu.Unlock()

	yield := AcquireStmtCloseYield()
	yield.Stmt = stmt.stmt
	yield.OnClose = func() {
		stmt.mu.Lock()
		stmt.closed = true
		cancel := stmt.cancelCleanup
		stmt.cancelCleanup = nil
		stmt.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	l.Push(yield)
	return -1
}
