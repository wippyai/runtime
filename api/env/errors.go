package env

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Errors returned by env operations.
var (
	ErrVariableNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "environment variable not found",
		retryable: apierror.False,
	}

	ErrStorageNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "environment storage backend not found",
		retryable: apierror.False,
	}

	ErrVariableReadOnly = &Error{
		kind:      apierror.KindPermissionDenied,
		message:   "environment variable is read-only",
		retryable: apierror.False,
	}

	ErrInvalidVariableName = &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid environment variable name",
		retryable: apierror.False,
	}

	ErrInvalidStorageID = &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid storage ID format, must have both namespace and name",
		retryable: apierror.False,
	}
)

// Error represents an env error with additional metadata.
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

// NewVariableNotFoundError creates a variable not found error with details.
func NewVariableNotFoundError(name string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "environment variable not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"variable": name}),
	}
}

// NewStorageNotFoundError creates a storage not found error with details.
func NewStorageNotFoundError(storageID string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "environment storage backend not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"storage": storageID}),
	}
}

// NewInvalidVariableNameError creates an invalid variable name error with details.
func NewInvalidVariableNameError(name string, reason string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid environment variable name: " + reason,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"variable": name, "reason": reason}),
	}
}
