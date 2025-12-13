package fs

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for filesystem operations.
var (
	ErrClosed           = errors.New("filesystem is closed")
	ErrPermissionDenied = errors.New("permission denied")
	ErrInvalidFileMode  = errors.New("invalid file mode: contains bits outside of fs.ModePerm")
)

// Error implements apierror.Error for filesystem errors.
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

// NewUnsupportedEntryKindError creates an error for unsupported entry kinds.
func NewUnsupportedEntryKindError(kind string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

// NewDecodeConfigError creates an error when config decoding fails.
func NewDecodeConfigError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode config",
		retryable: apierror.False,
		cause:     err,
	}
}

// NewFilesystemAlreadyExistsError creates an error when filesystem already exists.
func NewFilesystemAlreadyExistsError(id string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "filesystem " + id + " already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

// NewFilesystemNotFoundError creates an error when filesystem is not found.
func NewFilesystemNotFoundError(id string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "filesystem " + id + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

// NewFilesystemNotFoundWithCauseError creates an error when filesystem is not found with underlying cause.
func NewFilesystemNotFoundWithCauseError(id string, err error) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "filesystem not found: " + id,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
		cause:     err,
	}
}

// NewGetFilesystemError creates an error when getting filesystem fails.
func NewGetFilesystemError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get filesystem",
		retryable: apierror.Unknown,
		cause:     err,
	}
}

// NewCreateFilesystemError creates an error when creating filesystem fails.
func NewCreateFilesystemError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create filesystem",
		retryable: apierror.Unknown,
		cause:     err,
	}
}

// NewInvalidPathError creates an error for invalid paths.
func NewInvalidPathError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid path",
		retryable: apierror.False,
		cause:     err,
	}
}

// NewCreateDirectoryError creates an error when directory creation fails.
func NewCreateDirectoryError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create directory",
		retryable: apierror.Unknown,
		cause:     err,
	}
}

// NewOpenDirectoryError creates an error when opening directory fails.
func NewOpenDirectoryError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to open directory",
		retryable: apierror.Unknown,
		cause:     err,
	}
}

// NewUnsupportedOperationError creates an error for unsupported operations.
func NewUnsupportedOperationError(operation string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   operation + ": operation not supported",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"operation": operation}),
	}
}

// NewEmptyPathError creates an error when path is empty.
func NewEmptyPathError() *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "path cannot be empty",
		retryable: apierror.False,
	}
}

// NewNilReaderError creates an error when reader is nil.
func NewNilReaderError() *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "reader cannot be nil",
		retryable: apierror.False,
	}
}

// NewStatError creates an error when stat operation fails.
func NewStatError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "stat failed",
		retryable: apierror.Unknown,
		cause:     err,
	}
}

// NewOpenError creates an error when open operation fails.
func NewOpenError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "open failed",
		retryable: apierror.Unknown,
		cause:     err,
	}
}

// NewGetEmbeddedFilesystemError creates an error when getting embedded filesystem fails.
func NewGetEmbeddedFilesystemError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get embedded filesystem",
		retryable: apierror.Unknown,
		cause:     err,
	}
}

// NewEmptyPackPathError creates an error when pack path is empty.
func NewEmptyPackPathError() *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "packPath cannot be empty",
		retryable: apierror.False,
	}
}

// NewReadOnlyOperationError creates an error for unsupported operations on read-only filesystem.
func NewReadOnlyOperationError(operation string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   operation + ": operation not supported on read-only filesystem",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"operation": operation}),
	}
}

// NewPermissionDeniedError creates an error when permission is denied.
func NewPermissionDeniedError(required, ownerMode any, cause error) *Error {
	return &Error{
		kind:      apierror.KindPermissionDenied,
		message:   "permission denied",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"required": required, "ownerMode": ownerMode}),
		cause:     cause,
	}
}
