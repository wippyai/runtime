package process

import (
	"github.com/wippyai/runtime/api/attrs"
)

// Error kind constants.
const (
	LimitExceeded Kind = "LimitExceeded"
	NotFound      Kind = "NotFound"
	InvalidState  Kind = "InvalidState"
	Internal      Kind = "Internal"
)

// Errors returned by process operations.
var (
	ErrMaxProcessesExceeded = &Error{
		kind:    LimitExceeded,
		message: "max processes limit exceeded",
	}

	ErrProcessClosed = &Error{
		kind:    InvalidState,
		message: "process closed",
	}

	ErrProcessNotFound = &Error{
		kind:    NotFound,
		message: "process not found",
	}

	ErrProcessNotIdle = &Error{
		kind:    InvalidState,
		message: "process is not idle",
	}

	ErrSchedulerStopping = &Error{
		kind:    InvalidState,
		message: "scheduler is stopping",
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

func (e *Error) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}
func (e *Error) Kind() Kind                { return e.kind }
func (e *Error) Details() attrs.Attributes { return e.details }
func (e *Error) Unwrap() error             { return e.cause }

// NewError creates a new process error with the given kind and message.
func NewError(kind Kind, message string) *Error {
	return &Error{kind: kind, message: message}
}

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
