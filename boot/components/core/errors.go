package core

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable     = apierror.New(apierror.Internal, "logger not available in context").WithRetryable(apierror.False)
	ErrEventBusNotAvailable   = apierror.New(apierror.Internal, "event bus not available in context").WithRetryable(apierror.False)
	ErrRegistryNotAvailable   = apierror.New(apierror.Internal, "registry not available in context").WithRetryable(apierror.False)
	ErrTranscoderNotAvailable = apierror.New(apierror.Internal, "transcoder not available in context").WithRetryable(apierror.False)
)

func NewHistoryPathError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to resolve history path").WithCause(cause)
}

func NewSQLiteHistoryError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create SQLite history").WithCause(cause)
}
