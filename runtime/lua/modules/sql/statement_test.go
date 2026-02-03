package sql

import (
	"database/sql"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestCheckStatementValid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	stmt := &Statement{
		stmt:   &sql.Stmt{},
		closed: false,
	}

	ud := l.NewUserData()
	ud.Value = stmt
	l.Push(ud)

	result := checkStatement(l, 1)
	if result == nil {
		t.Error("expected non-nil Statement")
	}
	if result != stmt {
		t.Error("expected same Statement instance")
	}
}

func TestStatementCloseAlreadyClosed(t *testing.T) {
	stmt := &Statement{
		stmt:   nil,
		closed: true,
	}

	err := stmt.Close()
	if err != nil {
		t.Errorf("expected no error for already closed statement, got %v", err)
	}

	if !stmt.closed {
		t.Error("expected statement to remain closed")
	}
}
