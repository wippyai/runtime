package time

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

var ErrDurationNumberOrStringExpected = &Error{
	kind:      apierror.KindInvalid,
	message:   "duration: number or string expected",
	retryable: apierror.False,
}

func NewInvalidDurationType(got string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "duration expected, got " + got,
		retryable: apierror.False,
	}
}

func NewInvalidValueType(got string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "duration, string, or number expected, got " + got,
		retryable: apierror.False,
	}
}
