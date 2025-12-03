package process

import "github.com/wippyai/runtime/api/attrs"

// Error kind constants.
const (
	KindLimitExceeded Kind = "LimitExceeded"
)

// Errors returned by process operations.
var (
	ErrMaxProcessesExceeded = &Error{
		kind:    KindLimitExceeded,
		message: "max processes limit exceeded",
	}
)

type (
	// Kind categorizes process errors.
	Kind string

	// Error represents a process error with metadata.
	Error struct {
		kind    Kind
		message string
		details attrs.Attributes
		cause   error
	}
)

func (e *Error) Error() string             { return e.message }
func (e *Error) Kind() Kind                { return e.kind }
func (e *Error) Details() attrs.Attributes { return e.details }
func (e *Error) Unwrap() error             { return e.cause }

// WithCause returns a new error with the given cause.
func (e *Error) WithCause(cause error) *Error {
	return &Error{
		kind:    e.kind,
		message: e.message,
		details: e.details,
		cause:   cause,
	}
}

// WithDetails returns a new error with the given details.
func (e *Error) WithDetails(details attrs.Attributes) *Error {
	return &Error{
		kind:    e.kind,
		message: e.message,
		details: details,
		cause:   e.cause,
	}
}
