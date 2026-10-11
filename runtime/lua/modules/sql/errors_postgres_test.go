// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	lua "github.com/wippyai/go-lua"
)

func TestPostgresErrorClassification(t *testing.T) {
	cases := []struct {
		code      string
		kind      lua.Kind
		retryable lua.Ternary
	}{
		{"23505", lua.Conflict, lua.TernaryFalse},
		{"23P01", lua.Conflict, lua.TernaryFalse},
		{"23503", lua.Invalid, lua.TernaryFalse},
		{"23514", lua.Invalid, lua.TernaryFalse},
		{"23502", lua.Invalid, lua.TernaryFalse},
		{"23000", lua.Invalid, lua.TernaryFalse},
		{"22003", lua.Invalid, lua.TernaryFalse},
		{"42601", lua.Invalid, lua.TernaryFalse},
		{"42P01", lua.NotFound, lua.TernaryFalse},
		{"42703", lua.NotFound, lua.TernaryFalse},
		{"42704", lua.NotFound, lua.TernaryFalse},
		{"42501", lua.PermissionDenied, lua.TernaryFalse},
		{"28P01", lua.PermissionDenied, lua.TernaryFalse},
		{"40001", lua.Conflict, lua.TernaryTrue},
		{"40P01", lua.Conflict, lua.TernaryTrue},
		{"40003", lua.Conflict, lua.TernaryFalse},
		{"55P03", lua.Conflict, lua.TernaryTrue},
		{"55006", lua.Conflict, lua.TernaryFalse},
		{"08006", lua.Unavailable, lua.TernaryTrue},
		{"57P01", lua.Unavailable, lua.TernaryTrue},
		{"57P02", lua.Unavailable, lua.TernaryTrue},
		{"57P03", lua.Unavailable, lua.TernaryTrue},
		{"53300", lua.Unavailable, lua.TernaryTrue},
		{"57014", lua.Canceled, lua.TernaryFalse},
		{"0A000", lua.Invalid, lua.TernaryFalse},
		{"XX999", lua.Unknown, lua.TernaryUnknown},
		{"", lua.Unknown, lua.TernaryUnknown},
		{"4", lua.Unknown, lua.TernaryUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			native := &pgconn.PgError{Code: tc.code, Message: "opaque driver message", SchemaName: "public", TableName: "records", ColumnName: "value", ConstraintName: "records_value_key", DataTypeName: "text"}
			wrapped := wrapSQLError(l, fmt.Errorf("driver wrapper: %w", native), "query")
			if wrapped.Kind() != tc.kind || wrapped.Retryable() != tc.retryable {
				t.Fatalf("kind/retryable = %q/%v, want %q/%v", wrapped.Kind(), wrapped.Retryable(), tc.kind, tc.retryable)
			}
			for key, expected := range map[string]any{"sqlstate": tc.code, "schema": "public", "table": "records", "column": "value", "constraint": "records_value_key", "data_type": "text"} {
				if wrapped.Details()[key] != expected {
					t.Errorf("%s = %v, want %v", key, wrapped.Details()[key], expected)
				}
			}
			if !errors.Is(wrapped, native) {
				t.Fatal("native cause is lost")
			}
			l.SetGlobal("sql_error", wrapped)
			if err := l.DoString(fmt.Sprintf(`assert(sql_error:details().sqlstate == %q); assert(sql_error:details().constraint == "records_value_key")`, tc.code)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresWrappingPreservesMetadata(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	native := &pgconn.PgError{Code: "40001", Message: "opaque driver message"}
	enriched := lua.WrapError(native, "driver").WithKind(lua.Internal).WithRetryable(false).WithDetails(map[string]any{"operation_id": "original"})
	wrapped := wrapSQLError(l, enriched, "query")
	if wrapped.Kind() != lua.Internal || wrapped.Retryable() != lua.TernaryFalse || wrapped.Details()["operation_id"] != "original" || wrapped.Details()["sqlstate"] != "40001" {
		t.Fatalf("lost metadata: %v", wrapped.Details())
	}
	wrapped.Details()["operation_id"] = "changed"
	if enriched.Details()["operation_id"] != "original" {
		t.Fatal("input metadata mutated")
	}
	if _, ok := enriched.Details()["sqlstate"]; ok {
		t.Fatal("input metadata mutated")
	}
	if _, ok := wrapped.Details()["constraint"]; ok {
		t.Fatal("absent constraint invented")
	}
	if !errors.Is(wrapped, native) {
		t.Fatal("native cause is lost")
	}
}
