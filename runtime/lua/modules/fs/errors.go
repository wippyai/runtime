// SPDX-License-Identifier: MPL-2.0

package fs

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var ErrFileAlreadyClosed = apierror.New(apierror.Invalid, "file already closed").WithRetryable(apierror.False)
var ErrNullBytePath = apierror.New(apierror.Invalid, "path contains null byte").WithRetryable(apierror.False)
var ErrPathTraversal = apierror.New(apierror.Invalid, "path traversal not allowed").WithRetryable(apierror.False)

func NewReadError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "read failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewWriteError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "write failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewSeekError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "seek failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewStatError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "stat failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewSyncError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "sync failed").WithCause(cause).WithRetryable(apierror.False)
}
