package storage

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable           = apierror.New(apierror.KindInternal, "logger not available in context").WithRetryable(apierror.False)
	ErrTranscoderNotAvailable       = apierror.New(apierror.KindInternal, "transcoder not available in context").WithRetryable(apierror.False)
	ErrEventBusNotAvailable         = apierror.New(apierror.KindInternal, "event bus not available in context").WithRetryable(apierror.False)
	ErrResourceRegistryNotAvailable = apierror.New(apierror.KindInternal, "resource registry not available in context").WithRetryable(apierror.False)
	ErrSecurityRegistryNotAvailable = apierror.New(apierror.KindInternal, "security registry not available in context").WithRetryable(apierror.False)
	ErrHandlerRegistryNotAvailable  = apierror.New(apierror.KindInternal, "handler registry not available in context").WithRetryable(apierror.False)
	ErrRegistryNotAvailable         = apierror.New(apierror.KindInternal, "registry not available in context").WithRetryable(apierror.False)
)

func NewSQLManagerError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create sql manager").WithCause(cause)
}
