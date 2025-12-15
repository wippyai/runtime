package fs

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber: "+err.Error()).
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewGetFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get filesystem").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewCreateFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create filesystem").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewCreateDirectoryError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create directory").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewOpenDirectoryError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to open directory").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewStatError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "stat failed").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewOpenError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "open failed").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewGetEmbeddedFilesystemError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get embedded filesystem").WithCause(err).WithRetryable(apierror.Unspecified)
}

func NewUnsupportedEntryKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewDecodeConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode config").WithCause(err).WithRetryable(apierror.False)
}

func NewFilesystemAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "filesystem "+id+" already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewFilesystemNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "filesystem "+id+" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewFilesystemNotFoundWithCauseError(id string, err error) apierror.Error {
	return apierror.New(apierror.NotFound, "filesystem not found: "+id).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id})).
		WithCause(err)
}

func NewInvalidPathError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid path").WithCause(err).WithRetryable(apierror.False)
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
		WithDetails(attrs.NewBagFrom(map[string]any{"required": required, "ownerMode": ownerMode})).
		WithCause(cause)
}
