package sql

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newSQLError creates a new SQL error with metadata.
func newSQLError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newSQLQueryError creates an error for query execution failures.
func newSQLQueryError(l *lua.LState, err error, query string) lua.LValue {
	details := attrs.NewBag()
	if query != "" {
		details.Set("query", query)
	}

	retryable := isSQLErrorRetryable(err)
	kind := getSQLErrorKind(err)

	return newSQLError(l, kind, retryable, err.Error(), details)
}

// newSQLTransactionError creates an error for transaction failures.
func newSQLTransactionError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}

	retryable := isSQLErrorRetryable(err)
	kind := getSQLErrorKind(err)

	return newSQLError(l, kind, retryable, err.Error(), details)
}

// newSQLInvalidError creates an error for invalid SQL or parameters.
func newSQLInvalidError(l *lua.LState, msg string, query string) lua.LValue {
	details := attrs.NewBag()
	if query != "" {
		details.Set("query", query)
	}
	retryable := false
	return newSQLError(l, apierr.KindInvalid, &retryable, msg, details)
}

// newSQLResourceError creates an error for resource acquisition failures.
func newSQLResourceError(l *lua.LState, err error, resourceID string) lua.LValue {
	details := attrs.NewBag()
	if resourceID != "" {
		details.Set("resource_id", resourceID)
	}
	retryable := false
	return newSQLError(l, apierr.KindNotFound, &retryable, err.Error(), details)
}

// isSQLErrorRetryable determines if a SQL error is retryable.
func isSQLErrorRetryable(err error) *bool {
	if err == nil {
		return nil
	}

	retryable := false
	notRetryable := false

	if errors.Is(err, sql.ErrNoRows) {
		return &notRetryable
	}

	if errors.Is(err, sql.ErrTxDone) {
		return &notRetryable
	}

	if errors.Is(err, sql.ErrConnDone) {
		return &retryable
	}

	return nil
}

// getSQLErrorKind determines the error kind based on the SQL error.
func getSQLErrorKind(err error) apierr.Kind {
	if err == nil {
		return apierr.KindUnknown
	}

	if errors.Is(err, sql.ErrNoRows) {
		return apierr.KindNotFound
	}

	if errors.Is(err, sql.ErrTxDone) {
		return apierr.KindInvalid
	}

	if errors.Is(err, sql.ErrConnDone) {
		return apierr.KindUnavailable
	}

	return apierr.KindInternal
}
