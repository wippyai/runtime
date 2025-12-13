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

	ErrEmptyStorageList = &Error{
		kind:      apierror.KindInvalid,
		message:   "router storage must have at least one storage",
		retryable: apierror.False,
	}

	ErrStorageReadOnly = &Error{
		kind:      apierror.KindPermissionDenied,
		message:   "storage is read-only",
		retryable: apierror.False,
	}

	ErrNoStorages = &Error{
		kind:      apierror.KindInvalid,
		message:   "at least one storage must be provided",
		retryable: apierror.False,
	}

	ErrEmptyFilePath = &Error{
		kind:      apierror.KindInvalid,
		message:   "file path must not be empty",
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

// NewSubscriberError creates an error for subscriber creation failure.
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewInvalidVariableError creates an error for invalid variable.
func NewInvalidVariableError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid variable: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewVariableNameExistsError creates an error when variable name already exists.
func NewVariableNameExistsError(name string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "variable name already exists: " + name,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"name": name}),
	}
}

// NewInvalidStorageTypeError creates an error for invalid storage type.
func NewInvalidStorageTypeError(id string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "invalid storage type for " + id,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

// NewUnsupportedKindError creates an error for unsupported entry kind.
func NewUnsupportedKindError(kind string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

// NewDecodeConfigError creates an error for config decoding failure.
func NewDecodeConfigError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode configuration",
		retryable: apierror.False,
		cause:     err,
	}
}

// NewInvalidConfigError creates an error for invalid configuration.
func NewInvalidConfigError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid configuration",
		retryable: apierror.False,
		cause:     err,
	}
}

// NewStorageNotExistsError creates an error when storage does not exist.
func NewStorageNotExistsError(storageID string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "storage does not exist",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"storage_id": storageID}),
	}
}

// NewDecodeVariableError creates an error for variable decoding failure.
func NewDecodeVariableError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode variable",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewCreateStorageError creates an error for storage creation failure.
func NewCreateStorageError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create storage",
		retryable: apierror.False,
		cause:     err,
	}
}

// NewRenameTempFileError creates an error when renaming temp file fails.
func NewRenameTempFileError(attempts int, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to rename temp file",
		retryable: apierror.Unknown,
		details:   attrs.NewBagFrom(map[string]any{"attempts": attempts}),
		cause:     err,
	}
}

// NewRenameTempFileAfterRemoveError creates an error when renaming temp file after removing target fails.
func NewRenameTempFileAfterRemoveError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to rename temp file after removing target",
		retryable: apierror.Unknown,
		cause:     err,
	}
}
