//go:build cgo

// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/mattn/go-sqlite3"
	lua "github.com/wippyai/go-lua"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

type errorResult interface {
	HandleResult(*lua.LState, any, error) []lua.LValue
}

func TestSQLiteCodesAtLuaBoundary(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	for _, statement := range []string{"PRAGMA foreign_keys=ON", "CREATE TABLE parent(id INTEGER PRIMARY KEY)", "CREATE TABLE child(id INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED)", "CREATE TRIGGER rejected BEFORE INSERT ON parent BEGIN SELECT RAISE(ABORT, 'rejected by trigger'); END"} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	_, statementErr := db.ExecContext(ctx, "INSERT INTO parent VALUES(1)")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO child VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	commitErr := tx.Commit()
	for _, native := range []error{statementErr, commitErr} {
		var driver sqlite3.Error
		if !errors.As(native, &driver) {
			t.Fatalf("expected SQLite error, got %v", native)
		}
		handlers := []struct {
			yield    errorResult
			response any
			name     string
		}{
			{&QueryYield{}, sqlapi.QueryResponse{Error: native}, "query"},
			{&ExecuteYield{}, sqlapi.ExecuteResponse{Error: native}, "execute"},
			{&PrepareYield{}, sqlapi.PrepareResponse{Error: native}, "prepare"},
			{&BeginYield{}, sqlapi.BeginResponse{Error: native}, "begin"},
			{&StmtQueryYield{}, sqlapi.QueryResponse{Error: native}, "stmt query"},
			{&StmtExecuteYield{}, sqlapi.ExecuteResponse{Error: native}, "stmt execute"},
			{&StmtCloseYield{}, nil, "stmt close"},
			{&TxQueryYield{}, sqlapi.QueryResponse{Error: native}, "tx query"},
			{&TxExecuteYield{}, sqlapi.ExecuteResponse{Error: native}, "tx execute"},
			{&TxSavepointYield{}, sqlapi.ExecuteResponse{Error: native}, "savepoint"},
			{&TxPrepareYield{}, sqlapi.PrepareResponse{Error: native}, "tx prepare"},
			{&TxCommitYield{}, nil, "tx commit"},
			{&TxRollbackYield{}, nil, "tx rollback"},
		}
		for _, handler := range handlers {
			t.Run(fmt.Sprintf("%d/%s", driver.ExtendedCode, handler.name), func(t *testing.T) {
				l := lua.NewState()
				defer l.Close()
				check := func(values []lua.LValue) {
					t.Helper()
					e, ok := values[1].(*lua.Error)
					if !ok {
						t.Fatalf("expected Lua error, got %v", values)
					}
					if e.Details()["sqlite_code"] != int(driver.Code) || e.Details()["sqlite_extended_code"] != int(driver.ExtendedCode) {
						t.Fatalf("missing SQLite codes: %v", e.Details())
					}
					if !errors.Is(e, native) {
						t.Fatal("native cause was lost")
					}
					l.SetGlobal("sql_error", e)
					if err := l.DoString(fmt.Sprintf(`assert(sql_error:details().sqlite_code == %d); assert(sql_error:details().sqlite_extended_code == %d)`, driver.Code, driver.ExtendedCode)); err != nil {
						t.Fatal(err)
					}
				}
				check(handler.yield.HandleResult(l, nil, fmt.Errorf("driver wrapper: %w", native)))
				if handler.response != nil {
					check(handler.yield.HandleResult(l, handler.response, nil))
				}
			})
		}
	}
}

func TestSQLWrappingPreservesSQLiteMetadata(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	native := sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintForeignKey}
	enriched := lua.WrapError(native, "driver").WithKind(lua.Invalid).WithRetryable(false).
		WithDetails(map[string]any{"operation_id": "original"})
	wrapped := wrapSQLError(l, enriched, "query")
	if wrapped.Kind() != lua.Invalid || wrapped.Retryable() != lua.TernaryFalse || wrapped.Details()["operation_id"] != "original" || wrapped.Details()["sqlite_code"] != 19 || wrapped.Details()["sqlite_extended_code"] != 787 {
		t.Fatalf("lost metadata: %v", wrapped.Details())
	}
	if _, ok := enriched.Details()["sqlite_code"]; ok {
		t.Fatal("input metadata mutated")
	}
	if _, ok := enriched.Details()["sqlite_extended_code"]; ok {
		t.Fatal("input metadata mutated")
	}
	if !errors.Is(wrapped, native) {
		t.Fatal("native cause was lost")
	}
}
