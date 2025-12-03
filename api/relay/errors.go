package relay

import "github.com/wippyai/runtime/api/attrs"

// Error kind constants.
const (
	KindAlreadyExists Kind = "AlreadyExists"
	KindNotFound      Kind = "NotFound"
	KindInvalid       Kind = "Invalid"
)

// Errors returned by relay operations.
var (
	ErrAlreadyAttached = &Error{
		kind:    KindAlreadyExists,
		message: "receiver already attached",
	}

	ErrHostNotFound = &Error{
		kind:    KindNotFound,
		message: "host not found",
	}

	ErrHostAlreadyExists = &Error{
		kind:    KindAlreadyExists,
		message: "host already exists",
	}

	ErrInvalidPIDFormat = &Error{
		kind:    KindInvalid,
		message: "invalid pid format",
	}
)

type (
	// Kind categorizes relay errors.
	Kind string

	// Error represents a relay error with metadata.
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

// WithMessage returns a new error with a custom message.
func (e *Error) WithMessage(msg string) *Error {
	return &Error{
		kind:    e.kind,
		message: msg,
		details: e.details,
		cause:   e.cause,
	}
}
