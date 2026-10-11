// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"

	"github.com/jackc/pgx/v5/pgconn"
	lua "github.com/wippyai/go-lua"
)

func wrapSQLError(l *lua.LState, cause error, operation string) *lua.Error {
	wrapped := lua.WrapErrorWithLua(l, cause, operation)
	if wrapped == nil {
		return nil
	}
	var native *pgconn.PgError
	if errors.As(cause, &native) {
		details := wrapped.Details()
		if details == nil {
			details = make(map[string]any)
		}
		details["sqlstate"] = native.Code
		for key, value := range map[string]string{"schema": native.SchemaName, "table": native.TableName, "column": native.ColumnName, "constraint": native.ConstraintName, "data_type": native.DataTypeName} {
			if value != "" {
				details[key] = value
			}
		}
		wrapped = wrapped.WithDetails(details)
		kind, retryable := postgresErrorKind(native.Code)
		return classifySQLError(wrapped, kind, retryable)
	}
	switch {
	case errors.Is(cause, context.Canceled):
		return classifySQLError(wrapped, lua.Canceled, false)
	case errors.Is(cause, context.DeadlineExceeded):
		return classifySQLError(wrapped, lua.Timeout, false)
	case errors.Is(cause, driver.ErrBadConn), errors.Is(cause, io.EOF):
		return classifySQLError(wrapped, lua.Unavailable, true)
	case errors.Is(cause, io.ErrUnexpectedEOF):
		return classifySQLError(wrapped, lua.Unavailable, false)
	}
	var transport *net.OpError
	if errors.As(cause, &transport) {
		if transport.Timeout() {
			return classifySQLError(wrapped, lua.Timeout, false)
		}
		return classifySQLError(wrapped, lua.Unavailable, false)
	}
	return wrapSQLiteError(wrapped, cause)
}

func classifySQLError(wrapped *lua.Error, kind lua.Kind, retryable bool) *lua.Error {
	if kind != lua.Unknown {
		if wrapped.Kind() == lua.Unknown {
			wrapped = wrapped.WithKind(kind)
		}
		if wrapped.Retryable() == lua.TernaryUnknown {
			wrapped = wrapped.WithRetryable(retryable)
		}
	}
	return wrapped
}

func postgresErrorKind(code string) (lua.Kind, bool) {
	switch code {
	case "23505", "23P01", "55006":
		return lua.Conflict, false
	case "40001", "40P01", "55P03":
		return lua.Conflict, true
	case "42P01", "42703", "42704":
		return lua.NotFound, false
	case "42501":
		return lua.PermissionDenied, false
	case "57P01", "57P02", "57P03":
		return lua.Unavailable, true
	case "57014":
		return lua.Canceled, false
	}
	if len(code) == 5 {
		switch code[:2] {
		case "08", "53":
			return lua.Unavailable, true
		case "0A", "22", "23", "42":
			return lua.Invalid, false
		case "28":
			return lua.PermissionDenied, false
		case "40":
			return lua.Conflict, false
		}
	}
	return lua.Unknown, false
}
