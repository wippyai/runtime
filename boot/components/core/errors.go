package core

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable   = apierror.New(apierror.KindInternal, "logger not available in context").WithRetryable(apierror.False)
	ErrEventBusNotAvailable = apierror.New(apierror.KindInternal, "event bus not available in context").WithRetryable(apierror.False)
	ErrRegistryNotAvailable = apierror.New(apierror.KindInternal, "registry not available in context").WithRetryable(apierror.False)
)

func NewHistoryPathError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to resolve history path").WithCause(cause)
}

func NewSQLiteHistoryError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create SQLite history").WithCause(cause)
}
