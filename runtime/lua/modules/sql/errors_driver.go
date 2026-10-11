// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	lua "github.com/wippyai/go-lua"
)

func wrapSQLError(l *lua.LState, cause error, operation string) *lua.Error {
	wrapped := lua.WrapErrorWithLua(l, cause, operation)
	if wrapped == nil {
		return nil
	}
	var native interface {
		error
		SQLState() string
	}
	if errors.As(cause, &native) {
		details := wrapped.Details()
		if details == nil {
			details = make(map[string]any)
		}
		details["sqlstate"] = native.SQLState()
		for key, value := range postgresErrorDetails(cause) {
			if value != "" {
				details[key] = value
			}
		}
		wrapped = wrapped.WithDetails(details)
		kind, retryable := postgresErrorKind(native.SQLState())
		return classifySQLError(wrapped, kind, retryable)
	}
	switch {
	case errors.Is(cause, context.Canceled):
		return classifySQLError(wrapped, lua.Canceled, false)
	case errors.Is(cause, context.DeadlineExceeded):
		return classifySQLError(wrapped, lua.Timeout, false)
	case errors.Is(cause, driver.ErrBadConn):
		return classifySQLError(wrapped, lua.Unavailable, true)
	case errors.Is(cause, io.EOF), errors.Is(cause, io.ErrUnexpectedEOF):
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

func postgresErrorDetails(cause error) map[string]string {
	var schema, table, column, constraint, dataType string
	var pgxError *pgconn.PgError
	var pqError *pq.Error
	switch {
	case errors.As(cause, &pgxError):
		schema, table, column, constraint, dataType = pgxError.SchemaName, pgxError.TableName, pgxError.ColumnName, pgxError.ConstraintName, pgxError.DataTypeName
	case errors.As(cause, &pqError):
		schema, table, column, constraint, dataType = pqError.Schema, pqError.Table, pqError.Column, pqError.Constraint, pqError.DataTypeName
	}
	return map[string]string{"schema": schema, "table": table, "column": column, "constraint": constraint, "data_type": dataType}
}

func postgresErrorKind(code string) (lua.Kind, bool) {
	switch code {
	case "08001", "08003":
		return lua.Unavailable, true
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
		case "08":
			// Connection loss does not establish whether a write commits.
			return lua.Unavailable, false
		case "53":
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
