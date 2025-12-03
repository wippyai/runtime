package directory

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

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

func newInvalidDirectoryPathError(cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid directory path",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newCreateDirectoryError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "create directory (auto_init=true)",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newFailedToOpenDirectoryError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to open directory",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newUnsupportedEntryKindError(kind string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

func newFailedToDecodeConfigError(cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newDirectoryAlreadyExistsError(id string) error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "directory " + id + " already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func newDirectoryNotFoundError(id string) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "directory " + id + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func newFailedToCreateFilesystemError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create filesystem",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newUnsupportedOperationError(operation string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "ReadOnlyFS." + operation + ": operation not supported",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"operation": operation}),
	}
}

func newReadOnlyFileOperationError(operation string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "readOnlyFile." + operation + ": operation not supported",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"operation": operation}),
	}
}

func newReadOnlyFSStatError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "ReadOnlyFS.Stat",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newReadOnlyFSOpenError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "ReadOnlyFS.Open",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}
