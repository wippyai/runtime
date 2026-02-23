// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"database/sql"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestExecutorToString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	wrapper := &queryExecutorWrapper{
		db:    nil,
		query: "SELECT * FROM users",
		args:  []any{},
	}

	ud := l.NewUserData()
	ud.Value = wrapper
	l.Push(ud)

	executorToString(l)

	result := l.Get(-1)
	str := string(result.(lua.LString))
	expected := "QueryExecutor: SELECT * FROM users [Args: 0]"
	if str != expected {
		t.Errorf("expected %s, got %s", expected, str)
	}
}

func TestExecutorWithDB(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	mockDB := &sql.DB{}
	wrapper := &queryExecutorWrapper{
		db:    mockDB,
		tx:    nil,
		query: "SELECT * FROM users WHERE id = ?",
		args:  []any{1},
	}

	if wrapper.db != mockDB {
		t.Error("expected db to be set")
	}
	if wrapper.tx != nil {
		t.Error("expected tx to be nil")
	}
}

func TestExecutorWithTx(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	mockTx := &sql.Tx{}
	wrapper := &queryExecutorWrapper{
		db:    nil,
		tx:    mockTx,
		query: "INSERT INTO users (name) VALUES (?)",
		args:  []any{"test"},
	}

	if wrapper.tx != mockTx {
		t.Error("expected tx to be set")
	}
	if wrapper.db != nil {
		t.Error("expected db to be nil")
	}
}

func TestExecutorToSQL(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	wrapper := &queryExecutorWrapper{
		db:    nil,
		query: "SELECT id, name FROM users WHERE active = ?",
		args:  []any{true},
	}

	ud := l.NewUserData()
	ud.Value = wrapper
	l.Push(ud)

	count := executorToSQL(l)
	if count != 2 {
		t.Errorf("expected 2 return values, got %d", count)
	}

	query := l.Get(-2)
	if string(query.(lua.LString)) != "SELECT id, name FROM users WHERE active = ?" {
		t.Errorf("unexpected query: %s", query)
	}

	argsTable := l.Get(-1).(*lua.LTable)
	if argsTable.Len() != 1 {
		t.Errorf("expected 1 arg, got %d", argsTable.Len())
	}
}
