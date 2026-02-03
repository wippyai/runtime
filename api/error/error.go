// Package error provides error categorization and retry metadata.
package error

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
)

// Kind constants for error categorization.
const (
	Unknown          Kind = "Unknown"
	NotFound         Kind = "NotFound"
	AlreadyExists    Kind = "AlreadyExists"
	Invalid          Kind = "Invalid"
	PermissionDenied Kind = "PermissionDenied"
	Unavailable      Kind = "Unavailable"
	Internal         Kind = "Internal"
	Canceled         Kind = "Canceled"
	Conflict         Kind = "Conflict"
	Timeout          Kind = "Timeout"
	RateLimited      Kind = "RateLimited"
)

// Ternary constants for retry decisions.
const (
	Unspecified Ternary = iota
	True
	False
)

type (
	// Error extends the standard error interface with categorization and retry metadata.
	// Domains implement this interface to provide rich error information that can be
	// passed across layers (Go ↔ Lua, API ↔ Services, HTTP, Cluster).
	Error interface {
		error

		// Kind returns the error category for semantic handling.
		Kind() Kind

		// Retryable indicates if the operation should be retried.
		// Returns Unknown to defer decision to outer layers (composition pattern).
		Retryable() Ternary

		// Details returns structured metadata about the error.
		// Keys and values are domain-specific.
		Details() attrs.Attributes
	}

	// Builder allows fluent construction of errors with optional fields.
	Builder interface {
		Error
		WithRetryable(Ternary) Builder
		WithDetails(attrs.Attributes) Builder
		WithCause(error) Builder
		WithMessage(string) Builder
	}

	// Kind categorizes errors semantically across all domains.
	Kind string

	// Ternary represents three-state logic for composable error handling.
	Ternary int
)

// String returns the string representation of Kind.
func (k Kind) String() string {
	return string(k)
}

// String returns the string representation of Ternary.
func (t Ternary) String() string {
	switch t {
	case True:
		return "True"
	case False:
		return "False"
	case Unspecified:
		return "Unspecified"
	default:
		return "Unspecified"
	}
}

// Bool converts Ternary to boolean (Unspecified becomes false).
func (t Ternary) Bool() bool {
	return t == True
}

// err is the concrete implementation of Error interface.
type err struct {
	details   attrs.Attributes
	cause     error
	kind      Kind
	message   string
	retryable Ternary
}

func (e *err) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}
func (e *err) Kind() Kind                { return e.kind }
func (e *err) Retryable() Ternary        { return e.retryable }
func (e *err) Details() attrs.Attributes { return e.details }
func (e *err) Unwrap() error             { return e.cause }

// Is implements errors.Is by comparing kind and message for semantic equality.
func (e *err) Is(target error) bool {
	var t *err
	if errors.As(target, &t) {
		return e.kind == t.kind && e.message == t.message
	}
	return false
}

// WithRetryable is a Builder method - immutable, return new copy.
func (e *err) WithRetryable(r Ternary) Builder {
	return &err{kind: e.kind, message: e.message, retryable: r, details: e.details, cause: e.cause}
}
func (e *err) WithDetails(d attrs.Attributes) Builder {
	return &err{kind: e.kind, message: e.message, retryable: e.retryable, details: d, cause: e.cause}
}
func (e *err) WithCause(c error) Builder {
	return &err{kind: e.kind, message: e.message, retryable: e.retryable, details: e.details, cause: c}
}
func (e *err) WithMessage(m string) Builder {
	return &err{kind: e.kind, message: m, retryable: e.retryable, details: e.details, cause: e.cause}
}

// New creates a new error with the given kind and message.
func New(kind Kind, message string) Builder {
	return &err{kind: kind, message: message, retryable: Unspecified}
}

// E creates a new error with full control over all fields.
func E(kind Kind, message string, retryable Ternary, details attrs.Attributes, cause error) Error {
	return &err{
		kind:      kind,
		message:   message,
		retryable: retryable,
		details:   details,
		cause:     cause,
	}
}

// WithDetails creates an error with details.
func WithDetails(kind Kind, message string, details attrs.Attributes) Error {
	return &err{kind: kind, message: message, details: details, retryable: Unspecified}
}

// SetCause returns a new error with the same properties but with the given cause.
func SetCause(e Error, cause error) Error {
	return &err{
		kind:      e.Kind(),
		message:   e.Error(),
		retryable: e.Retryable(),
		details:   e.Details(),
		cause:     cause,
	}
}

// SetMessage returns a new error with the same properties but with a new message.
func SetMessage(e Error, message string) Error {
	var cause error
	if u, ok := e.(interface{ Unwrap() error }); ok {
		cause = u.Unwrap()
	}
	return &err{
		kind:      e.Kind(),
		message:   message,
		retryable: e.Retryable(),
		details:   e.Details(),
		cause:     cause,
	}
}

// SetDetails returns a new error with the same properties but with new details.
// The original error is wrapped as cause so errors.Is works.
func SetDetails(e Error, details attrs.Attributes) Error {
	return &err{
		kind:      e.Kind(),
		message:   e.Error(),
		retryable: e.Retryable(),
		details:   details,
		cause:     e,
	}
}

// Rich extends Error with chain traversal and stack traces.
// Used for errors that cross process boundaries (Temporal, queues).
type Rich interface {
	error
	Kind() Kind
	Retryable() Ternary
	Details() map[string]any
	StackFrames() []string
	Unwrap() error
}

// RichError is the concrete implementation of Rich, used by FromChain.
type RichError struct {
	cause     error
	details   map[string]any
	message   string
	kind      Kind
	stack     []string
	retryable Ternary
}

func (e *RichError) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}

func (e *RichError) Kind() Kind              { return e.kind }
func (e *RichError) Retryable() Ternary      { return e.retryable }
func (e *RichError) Details() map[string]any { return e.details }
func (e *RichError) StackFrames() []string   { return e.stack }
func (e *RichError) Unwrap() error           { return e.cause }
func (e *RichError) Msg() string             { return e.message }

// NewRich creates a new Rich error.
func NewRich(kind Kind, message string) *RichError {
	return &RichError{kind: kind, message: message, retryable: Unspecified}
}

// WithRetryable sets the retryable flag.
func (e *RichError) WithRetryable(r Ternary) *RichError {
	e.retryable = r
	return e
}

// WithDetails sets the details map.
func (e *RichError) WithDetails(d map[string]any) *RichError {
	e.details = d
	return e
}

// WithStack sets the stack frames.
func (e *RichError) WithStack(s []string) *RichError {
	e.stack = s
	return e
}

// WithCause sets the wrapped error.
func (e *RichError) WithCause(c error) *RichError {
	e.cause = c
	return e
}
