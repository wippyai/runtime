package security

import "github.com/wippyai/runtime/api/attrs"

// Error kind constants.
const (
	KindNotFound Kind = "NotFound"
	KindInvalid  Kind = "Invalid"
	KindExpired  Kind = "Expired"
	KindRevoked  Kind = "Revoked"
	KindDenied   Kind = "Denied"
)

// Errors returned by security operations.
var (
	ErrNoFrameContext = &Error{
		kind:    KindInvalid,
		message: "no frame context available",
	}

	ErrScopeNotFound = &Error{
		kind:    KindNotFound,
		message: "security scope not found in context",
	}

	ErrRegistryNotFound = &Error{
		kind:    KindNotFound,
		message: "security registry not found in context",
	}

	ErrPolicyNotFound = &Error{
		kind:    KindNotFound,
		message: "policy not found",
	}

	ErrGroupNotFound = &Error{
		kind:    KindNotFound,
		message: "policy group not found",
	}

	ErrTokenInvalid = &Error{
		kind:    KindInvalid,
		message: "invalid token format",
	}

	ErrTokenExpired = &Error{
		kind:    KindExpired,
		message: "token expired",
	}

	ErrTokenRevoked = &Error{
		kind:    KindRevoked,
		message: "token revoked",
	}

	ErrTokenNotFound = &Error{
		kind:    KindNotFound,
		message: "token not found",
	}

	ErrUnsupportedTokenType = &Error{
		kind:    KindInvalid,
		message: "unsupported token type",
	}

	ErrPermissionDenied = &Error{
		kind:    KindDenied,
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
