package logger

import (
	apierror "github.com/wippyai/runtime/api/error"
)

func NewBuildLoggerError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to build logger").WithCause(cause).WithRetryable(apierror.False)
}
