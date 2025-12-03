package topology

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for topology operations.
var (
	ErrNameAlreadyRegistered = &Error{
		kind:    apierror.KindAlreadyExists,
		message: "name already registered",
	}

	ErrPIDNotFound = &Error{
		kind:    apierror.KindNotFound,
		message: "pid not found",
	}

	ErrPIDNotRegistered = &Error{
		kind:    apierror.KindNotFound,
		message: "pid not registered",
	}

	ErrAlreadyMonitoring = &Error{
		kind:    apierror.KindAlreadyExists,
		message: "already monitoring pid",
	}
)

// Error represents a topology error with metadata.
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

// Is implements errors.Is interface for sentinel error comparison.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.kind == t.kind && e.message == t.message
}

// WithCause returns a new error with the given cause.
func (e *Error) WithCause(cause error) *Error {
	return &Error{
		kind:      e.kind,
		message:   e.message,
		retryable: e.retryable,
		details:   e.details,
		cause:     cause,
	}
}

// WithDetails returns a new error with the given details.
func (e *Error) WithDetails(details attrs.Attributes) *Error {
	return &Error{
		kind:      e.kind,
		message:   e.message,
		retryable: e.retryable,
		details:   details,
		cause:     e.cause,
	}
}

// WithMessage returns a new error with a custom message.
func (e *Error) WithMessage(msg string) *Error {
	return &Error{
		kind:      e.kind,
		message:   msg,
		retryable: e.retryable,
		details:   e.details,
		cause:     e.cause,
	}
}
