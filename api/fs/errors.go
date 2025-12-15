package fs

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrClosed           = errors.New("filesystem is closed")
	ErrPermissionDenied = errors.New("permission denied")
	ErrInvalidFileMode  = errors.New("invalid file mode: contains bits outside of fs.ModePerm")
)

func NewUnsupportedEntryKindError(kind string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewDecodeConfigError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode config").WithCause(err).WithRetryable(apierror.False)
}

func NewFilesystemAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, "filesystem "+id+" already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewFilesystemNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "filesystem "+id+" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewFilesystemNotFoundWithCauseError(id string, err error) apierror.Error {
	return apierror.New(apierror.KindNotFound, "filesystem not found: "+id).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id})).
		WithCause(err)
}

func NewInvalidPathError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid path").WithCause(err).WithRetryable(apierror.False)
}

func NewUnsupportedOperationError(operation string) apierror.Error {
	return apierror.New(apierror.KindInvalid, operation+": operation not supported").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"operation": operation}))
}

func NewEmptyPathError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "path cannot be empty").WithRetryable(apierror.False)
}

func NewNilReaderError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "reader cannot be nil").WithRetryable(apierror.False)
}

func NewEmptyPackPathError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "packPath cannot be empty").WithRetryable(apierror.False)
}

func NewReadOnlyOperationError(operation string) apierror.Error {
	return apierror.New(apierror.KindInvalid, operation+": operation not supported on read-only filesystem").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"operation": operation}))
}

func NewPermissionDeniedError(required, ownerMode any, cause error) apierror.Error {
	return apierror.New(apierror.KindPermissionDenied, "permission denied").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"required": required, "ownerMode": ownerMode})).
		WithCause(cause)
}
