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
	for _, statement := range []string{"PRAGMA foreign_keys=ON", "CREATE TABLE parent(id INTEGER PRIMARY KEY)", "CREATE TABLE child(id INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED)", "CREATE TRIGGER rejected BEFORE INSERT ON parent BEGIN SELECT RAISE(ABORT, 'rejected by trigger'); END"} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	_, statementErr := db.Exec("INSERT INTO parent VALUES(1)")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO child VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	commitErr := tx.Commit()
	for _, native := range []error{statementErr, commitErr} {
		var driver sqlite3.Error
		if !errors.As(native, &driver) {
			t.Fatalf("expected SQLite error, got %v", native)
		}
		handlers := []struct {
			name     string
			yield    errorResult
			response any
		}{
			{"query", &QueryYield{}, sqlapi.QueryResponse{Error: native}},
			{"execute", &ExecuteYield{}, sqlapi.ExecuteResponse{Error: native}},
			{"prepare", &PrepareYield{}, sqlapi.PrepareResponse{Error: native}},
			{"begin", &BeginYield{}, sqlapi.BeginResponse{Error: native}},
			{"stmt query", &StmtQueryYield{}, sqlapi.QueryResponse{Error: native}},
			{"stmt execute", &StmtExecuteYield{}, sqlapi.ExecuteResponse{Error: native}},
			{"stmt close", &StmtCloseYield{}, nil},
			{"tx query", &TxQueryYield{}, sqlapi.QueryResponse{Error: native}},
			{"tx execute", &TxExecuteYield{}, sqlapi.ExecuteResponse{Error: native}},
			{"savepoint", &TxSavepointYield{}, sqlapi.ExecuteResponse{Error: native}},
			{"tx prepare", &TxPrepareYield{}, sqlapi.PrepareResponse{Error: native}},
			{"tx commit", &TxCommitYield{}, nil},
			{"tx rollback", &TxRollbackYield{}, nil},
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

func TestSQLWrappingPreservesMetadata(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	cause := lua.NewError("driver error").WithKind(lua.Unavailable).WithRetryable(true).WithDetails(map[string]any{"operation_id": "original"})
	wrapped := wrapSQLError(l, cause, "query")
	if wrapped.Kind() != lua.Unavailable || wrapped.Details()["operation_id"] != "original" {
		t.Fatalf("lost metadata: %v", wrapped)
	}
	if _, ok := wrapped.Details()["sqlite_code"]; ok {
		t.Fatal("non-SQLite error acquired SQLite codes")
	}
	native := sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintForeignKey}
	enriched := lua.WrapError(native, "driver").WithKind(lua.Invalid).WithDetails(map[string]any{"operation_id": "original"})
	wrapped = wrapSQLError(l, enriched, "query")
	if wrapped.Kind() != lua.Invalid || wrapped.Details()["operation_id"] != "original" || wrapped.Details()["sqlite_extended_code"] != 787 {
		t.Fatalf("lost metadata: %v", wrapped.Details())
	}
	if _, ok := enriched.Details()["sqlite_code"]; ok {
		t.Fatal("input metadata mutated")
	}
}
