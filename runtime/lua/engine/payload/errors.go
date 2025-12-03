package payload

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

func NewInvalidFormatError(message string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   message,
		retryable: apierror.False,
	}
}

func NewInvalidTypeError(message string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   message,
		retryable: apierror.False,
	}
}

func NewTranscodeError(message string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   message,
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewConversionError(message string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   message,
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewUnsupportedTypeError(message string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   message,
		retryable: apierror.False,
	}
}
