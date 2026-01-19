package fs

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewGetFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get filesystem").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewCreateFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create filesystem").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewCreateDirectoryError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create directory").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewOpenDirectoryError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to open directory").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewStatError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "stat failed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewOpenError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "open failed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewGetEmbeddedFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get embedded filesystem").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewUnsupportedEntryKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewDecodeConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewFilesystemAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "filesystem "+id+" already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"filesystem_id": id}))
}

func NewFilesystemNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "filesystem "+id+" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"filesystem_id": id}))
}

func NewFilesystemNotFoundWithCauseError(id string, err error) apierror.Error {
	return apierror.New(apierror.NotFound, "filesystem not found: "+id).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"filesystem_id": id, "cause": err.Error()})).
		WithCause(err)
}

func NewInvalidPathError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid path").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewUnsupportedOperationError(operation string) apierror.Error {
	return apierror.New(apierror.Invalid, operation+": operation not supported").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"operation": operation}))
}

func NewEmptyPathError() apierror.Error {
	return apierror.New(apierror.Invalid, "path cannot be empty").WithRetryable(apierror.False)
}

func NewNilReaderError() apierror.Error {
	return apierror.New(apierror.Invalid, "reader cannot be nil").WithRetryable(apierror.False)
}

func NewEmptyPackPathError() apierror.Error {
	return apierror.New(apierror.Invalid, "packPath cannot be empty").WithRetryable(apierror.False)
}

func NewReadOnlyOperationError(operation string) apierror.Error {
	return apierror.New(apierror.Invalid, operation+": operation not supported on read-only filesystem").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"operation": operation}))
}

func NewPermissionDeniedError(required, ownerMode any, cause error) apierror.Error {
	return apierror.New(apierror.PermissionDenied, "permission denied").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"required": required, "ownerMode": ownerMode, "cause": cause.Error()})).
		WithCause(cause)
}
