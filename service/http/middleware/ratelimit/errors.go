package ratelimit

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

func NewInvalidDurationError(s string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid duration: " + s,
	}
}

func NewInvalidDurationValueError(s string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid duration value: " + s,
	}
}

func NewInvalidDurationUnitError(unit string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid duration unit: " + unit + " (use s, m, or h)",
	}
}
