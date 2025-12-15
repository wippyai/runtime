package logger

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrInvalidLogLevel = apierror.New(apierror.Invalid, "invalid log level").WithRetryable(apierror.False)

	ErrInvalidLogFormat = apierror.New(apierror.Invalid, "invalid log format").WithRetryable(apierror.False)
)

func NewCreateLoggerError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create logger").WithCause(cause).WithRetryable(apierror.False)
}

func NewBuildLoggerError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to build logger").WithCause(cause).WithRetryable(apierror.False)
}
