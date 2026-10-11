//go:build cgo

// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"errors"

	"github.com/rqlite/go-sqlite3"
	lua "github.com/wippyai/go-lua"
)

func wrapSQLiteError(wrapped *lua.Error, cause error) *lua.Error {
	var native sqlite3.Error
	if !errors.As(cause, &native) {
		return wrapped
	}
	details := wrapped.Details()
	if details == nil {
		details = make(map[string]any)
	}
	details["sqlite_code"] = int(native.Code)
	details["sqlite_extended_code"] = int(native.ExtendedCode)
	wrapped = wrapped.WithDetails(details)
	kind, retryable := sqliteErrorKind(native)
	return classifySQLError(wrapped, kind, retryable)
}

func sqliteErrorKind(native sqlite3.Error) (lua.Kind, bool) {
	switch native.ExtendedCode {
	case sqlite3.ErrConstraintUnique, sqlite3.ErrConstraintPrimaryKey, sqlite3.ErrConstraintRowID:
		return lua.Conflict, false
	}
	switch native.Code {
	case sqlite3.ErrConstraint, sqlite3.ErrError, sqlite3.ErrMismatch, sqlite3.ErrRange, sqlite3.ErrTooBig:
		return lua.Invalid, false
	case sqlite3.ErrBusy, sqlite3.ErrLocked:
		return lua.Conflict, true
	case sqlite3.ErrIoErr:
		// An I/O failure can leave a write's outcome unknown.
		return lua.Unavailable, false
	case sqlite3.ErrCantOpen:
		return lua.Unavailable, true
	case sqlite3.ErrReadonly, sqlite3.ErrPerm, sqlite3.ErrAuth:
		return lua.PermissionDenied, false
	case sqlite3.ErrInterrupt:
		return lua.Canceled, false
	case sqlite3.ErrAbort:
		return lua.Conflict, false
	case sqlite3.ErrInternal, sqlite3.ErrCorrupt, sqlite3.ErrNotADB:
		return lua.Internal, false
	}
	return lua.Unknown, false
}
