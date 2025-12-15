package fs

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrFSIDRequired = apierror.New(apierror.KindInvalid, "filesystem ID is required").WithRetryable(apierror.False)

	ErrPathRequired = apierror.New(apierror.KindInvalid, "path is required").WithRetryable(apierror.False)
)

func NewFilesystemNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "filesystem not found: "+id).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"fs_id": id}))
}

func NewOpenFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to open file").WithCause(cause).WithRetryable(apierror.False)
}

var ErrFileAlreadyClosed = apierror.New(apierror.KindInvalid, "file already closed").WithRetryable(apierror.False)

func NewReadError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "read failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewWriteError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "write failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewSeekError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "seek failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewStatError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "stat failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewSyncError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "sync failed").WithCause(cause).WithRetryable(apierror.False)
}
