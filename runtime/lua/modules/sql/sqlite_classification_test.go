//go:build cgo

// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"errors"
	"fmt"
	"testing"

	"github.com/rqlite/go-sqlite3"
	lua "github.com/wippyai/go-lua"
)

func TestSQLiteErrorClassification(t *testing.T) {
	cases := []struct {
		kind      lua.Kind
		code      sqlite3.ErrNo
		extended  sqlite3.ErrNoExtended
		retryable lua.Ternary
	}{
		{lua.Conflict, sqlite3.ErrConstraint, sqlite3.ErrConstraintUnique, lua.TernaryFalse},
		{lua.Conflict, sqlite3.ErrConstraint, sqlite3.ErrConstraintPrimaryKey, lua.TernaryFalse},
		{lua.Conflict, sqlite3.ErrConstraint, sqlite3.ErrConstraintRowID, lua.TernaryFalse},
		{lua.Invalid, sqlite3.ErrConstraint, sqlite3.ErrConstraintForeignKey, lua.TernaryFalse},
		{lua.Invalid, sqlite3.ErrConstraint, sqlite3.ErrConstraintCheck, lua.TernaryFalse},
		{lua.Invalid, sqlite3.ErrConstraint, sqlite3.ErrConstraintNotNull, lua.TernaryFalse},
		{lua.Internal, sqlite3.ErrConstraint, sqlite3.ErrConstraintTrigger, lua.TernaryFalse},
		{lua.Internal, sqlite3.ErrConstraint, sqlite3.ErrConstraintCommitHook, lua.TernaryFalse},
		{lua.Internal, sqlite3.ErrConstraint, sqlite3.ErrConstraintFunction, lua.TernaryFalse},
		{lua.Internal, sqlite3.ErrConstraint, sqlite3.ErrConstraintVTab, lua.TernaryFalse},
		{lua.Invalid, sqlite3.ErrConstraint, sqlite3.ErrConstraint.Extend(99), lua.TernaryFalse},
		{lua.Conflict, sqlite3.ErrBusy, sqlite3.ErrBusySnapshot, lua.TernaryTrue},
		{lua.Conflict, sqlite3.ErrBusy, sqlite3.ErrBusyRecovery, lua.TernaryTrue},
		{lua.Conflict, sqlite3.ErrLocked, sqlite3.ErrLockedSharedCache, lua.TernaryTrue},
		{lua.Unavailable, sqlite3.ErrIoErr, sqlite3.ErrIoErrRead, lua.TernaryFalse},
		{lua.Unavailable, sqlite3.ErrCantOpen, sqlite3.ErrCantOpenIsDir, lua.TernaryTrue},
		{lua.Internal, sqlite3.ErrError, sqlite3.ErrError.Extend(0), lua.TernaryFalse},
		{lua.Invalid, sqlite3.ErrMismatch, sqlite3.ErrMismatch.Extend(0), lua.TernaryFalse},
		{lua.Invalid, sqlite3.ErrRange, sqlite3.ErrRange.Extend(0), lua.TernaryFalse},
		{lua.Invalid, sqlite3.ErrTooBig, sqlite3.ErrTooBig.Extend(0), lua.TernaryFalse},
		{lua.PermissionDenied, sqlite3.ErrReadonly, sqlite3.ErrReadonly.Extend(0), lua.TernaryFalse},
		{lua.PermissionDenied, sqlite3.ErrPerm, sqlite3.ErrPerm.Extend(0), lua.TernaryFalse},
		{lua.PermissionDenied, sqlite3.ErrAuth, sqlite3.ErrAuth.Extend(0), lua.TernaryFalse},
		{lua.Canceled, sqlite3.ErrInterrupt, sqlite3.ErrInterrupt.Extend(0), lua.TernaryFalse},
		{lua.Conflict, sqlite3.ErrAbort, sqlite3.ErrAbort.Extend(0), lua.TernaryFalse},
		{lua.Internal, sqlite3.ErrCorrupt, sqlite3.ErrCorrupt.Extend(0), lua.TernaryFalse},
		{lua.Internal, sqlite3.ErrNotADB, sqlite3.ErrNotADB.Extend(0), lua.TernaryFalse},
		{lua.Unknown, sqlite3.ErrNo(99), sqlite3.ErrNo(99).Extend(0), lua.TernaryUnknown},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.extended), func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			native := sqlite3.Error{Code: tc.code, ExtendedCode: tc.extended}
			wrapped := wrapSQLError(l, fmt.Errorf("driver wrapper: %w", native), "query")
			if wrapped.Kind() != tc.kind || wrapped.Retryable() != tc.retryable {
				t.Fatalf("kind/retryable = %q/%v, want %q/%v", wrapped.Kind(), wrapped.Retryable(), tc.kind, tc.retryable)
			}
			if wrapped.Details()["sqlite_code"] != int(tc.code) || wrapped.Details()["sqlite_extended_code"] != int(tc.extended) {
				t.Fatalf("lost SQLite codes: %v", wrapped.Details())
			}
			if !errors.Is(wrapped, native) {
				t.Fatal("native cause is lost")
			}
		})
	}
}
