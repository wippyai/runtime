package security

import "github.com/wippyai/runtime/api/attrs"

// Error kind constants.
const (
	NotFound Kind = "NotFound"
	Invalid  Kind = "Invalid"
	Expired  Kind = "Expired"
	Revoked  Kind = "Revoked"
	Denied   Kind = "Denied"
)

// Errors returned by security operations.
var (
	ErrNoFrameContext = &Error{
		kind:    Invalid,
		message: "no frame context available",
	}

	ErrScopeNotFound = &Error{
		kind:    NotFound,
		message: "security scope not found in context",
	}

	ErrRegistryNotFound = &Error{
		kind:    NotFound,
		message: "security registry not found in context",
	}

	ErrPolicyNotFound = &Error{
		kind:    NotFound,
		message: "policy not found",
	}

	ErrGroupNotFound = &Error{
		kind:    NotFound,
		message: "policy group not found",
	}

	ErrTokenInvalid = &Error{
		kind:    Invalid,
		message: "invalid token format",
	}

	ErrTokenExpired = &Error{
		kind:    Expired,
		message: "token expired",
	}

	ErrTokenRevoked = &Error{
		kind:    Revoked,
		message: "token revoked",
	}

	ErrTokenNotFound = &Error{
		kind:    NotFound,
		message: "token not found",
	}

	ErrUnsupportedTokenType = &Error{
		kind:    Invalid,
		message: "unsupported token type",
	}

	ErrPermissionDenied = &Error{
		kind:    Denied,
		message: "permission denied",
	}
)

type (
	// Kind categorizes security errors.
	Kind string

	// Error represents a security error with metadata.
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
