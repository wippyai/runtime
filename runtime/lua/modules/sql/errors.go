package sql

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error is a local error type for sql module that implements apierror.Error.
type Error struct {
	message   string
	kind      apierror.Kind
	retryable apierror.Ternary
	cause     error
	details   attrs.Attributes
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
