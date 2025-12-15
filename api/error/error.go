// Package error provides error categorization and retry metadata.
package error

import "github.com/wippyai/runtime/api/attrs"

// Kind constants for error categorization.
const (
	KindUnknown          Kind = "Unknown"
	KindNotFound         Kind = "NotFound"
	KindAlreadyExists    Kind = "AlreadyExists"
	KindInvalid          Kind = "Invalid"
	KindPermissionDenied Kind = "PermissionDenied"
	KindUnavailable      Kind = "Unavailable"
	KindInternal         Kind = "Internal"
	KindCanceled         Kind = "Canceled"
	KindConflict         Kind = "Conflict"
	KindTimeout          Kind = "Timeout"
	KindRateLimited      Kind = "RateLimited"
)

// Ternary constants for retry decisions.
const (
	Unknown Ternary = iota
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
	case Unknown:
		return "Unknown"
	default:
		return "Unknown"
	}
}

// Bool converts Ternary to boolean (Unknown becomes false).
func (t Ternary) Bool() bool {
	return t == True
}

// err is the concrete implementation of Error interface.
type err struct {
	kind      Kind
	message   string
	retryable Ternary
	details   attrs.Attributes
	cause     error
}

func (e *err) Error() string             { return e.message }
func (e *err) Kind() Kind                { return e.kind }
func (e *err) Retryable() Ternary        { return e.retryable }
func (e *err) Details() attrs.Attributes { return e.details }
func (e *err) Unwrap() error             { return e.cause }

// Is implements errors.Is by comparing kind and message for semantic equality.
func (e *err) Is(target error) bool {
	if t, ok := target.(*err); ok {
		return e.kind == t.kind && e.message == t.message
	}
	return false
}

// Builder methods - immutable, return new copy.
func (e *err) WithRetryable(r Ternary) *err {
	return &err{kind: e.kind, message: e.message, retryable: r, details: e.details, cause: e.cause}
}
func (e *err) WithDetails(d attrs.Attributes) *err {
	return &err{kind: e.kind, message: e.message, retryable: e.retryable, details: d, cause: e.cause}
}
func (e *err) WithCause(c error) *err {
	return &err{kind: e.kind, message: e.message, retryable: e.retryable, details: e.details, cause: c}
}
func (e *err) WithMessage(m string) *err {
	return &err{kind: e.kind, message: m, retryable: e.retryable, details: e.details, cause: e.cause}
}

// New creates a new error with the given kind and message.
func New(kind Kind, message string) *err {
	return &err{kind: kind, message: message, retryable: Unknown}
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

// Wrap creates a new error that wraps an existing error.
func Wrap(kind Kind, message string, cause error) Error {
	return &err{kind: kind, message: message, cause: cause, retryable: Unknown}
}

// WithDetails creates an error with details.
func WithDetails(kind Kind, message string, details attrs.Attributes) Error {
	return &err{kind: kind, message: message, details: details, retryable: Unknown}
}

// Retryable creates a retryable error.
func Retryable(kind Kind, message string, cause error) Error {
	return &err{kind: kind, message: message, cause: cause, retryable: True}
}

// NotRetryable creates a non-retryable error.
func NotRetryable(kind Kind, message string, cause error) Error {
	return &err{kind: kind, message: message, cause: cause, retryable: False}
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
