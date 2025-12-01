package env

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/env"
	apierror "github.com/wippyai/runtime/api/error"
)

// Re-export API errors for convenience
var (
	ErrVariableNotFound    = env.ErrVariableNotFound
	ErrStorageNotFound     = env.ErrStorageNotFound
	ErrVariableReadOnly    = env.ErrVariableReadOnly
	ErrInvalidVariableName = env.ErrInvalidVariableName
)

// Error implements apierror.Error for service-level env errors
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }

// Storage-specific errors
var (
	ErrStorageReadOnly = &Error{
		kind:      apierror.KindPermissionDenied,
		message:   "storage is read-only",
		retryable: apierror.False,
	}

	ErrStorageClosed = &Error{
		kind:      apierror.KindUnavailable,
		message:   "storage is closed",
		retryable: apierror.False,
	}

	ErrNoStorages = &Error{
		kind:      apierror.KindInvalid,
		message:   "at least one storage must be provided",
		retryable: apierror.False,
	}

	ErrInvalidConfig = &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid configuration",
		retryable: apierror.False,
	}

	ErrUnsupportedKind = &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind",
		retryable: apierror.False,
	}

	ErrStorageExists = &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "storage already exists",
		retryable: apierror.False,
	}

	ErrStorageNotExists = &Error{
		kind:      apierror.KindNotFound,
		message:   "storage does not exist",
		retryable: apierror.False,
	}

	ErrDecodeConfig = &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode configuration",
		retryable: apierror.False,
	}

	ErrCreateStorage = &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create storage",
		retryable: apierror.False,
	}

	ErrStorageNotWritable = &Error{
		kind:      apierror.KindPermissionDenied,
		message:   "storage does not support write operations",
		retryable: apierror.False,
	}
)
