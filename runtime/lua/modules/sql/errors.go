// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"errors"

	"github.com/mattn/go-sqlite3"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error is a local error type for sql module that implements apierror.Error.
type Error struct {
	cause     error
	details   attrs.Attributes
	message   string
	kind      apierror.Kind
	retryable apierror.Ternary
}

func (e *Error) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}

func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

func NewInvalidParametersTypeError(actualType string) apierror.Error {
	return &Error{
		message:   "parameters must be a table, got " + actualType,
		kind:      apierror.Invalid,
		retryable: apierror.False,
	}
}

// wrapSQLError retains driver result codes at the public Lua boundary.
func wrapSQLError(l *lua.LState, cause error, operation string) *lua.Error {
	wrapped := lua.WrapErrorWithLua(l, cause, operation)
	var native sqlite3.Error
	if errors.As(cause, &native) {
		details := make(map[string]any, len(wrapped.Details())+2)
		for key, value := range wrapped.Details() {
			details[key] = value
		}
		details["sqlite_code"] = int(native.Code)
		details["sqlite_extended_code"] = int(native.ExtendedCode)
		wrapped.WithDetails(details)
	}
	return wrapped
}
