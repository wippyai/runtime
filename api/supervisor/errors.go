package supervisor

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error kind constants specific to supervisor.
const (
	KindTerminated apierror.Kind = "Terminated"
	KindExited     apierror.Kind = "Exited"
)

// Errors returned by supervisor operations.
var (
	ErrTerminated = &Error{
		kind:      KindTerminated,
		message:   "service terminated",
		retryable: apierror.False,
	}

	ErrExit = &Error{
		kind:      KindExited,
		message:   "service exited",
		retryable: apierror.False,
	}
)

// Error represents a supervisor error with metadata.
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

// NewInvalidDurationError creates an error for invalid duration format.
func NewInvalidDurationError(field string, cause error) *Error {
	details := attrs.NewBag()
	details.Set("field", field)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid " + field + " duration format",
		retryable: apierror.False,
		details:   details,
		cause:     cause,
	}
}
