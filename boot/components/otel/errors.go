package otel

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable         = apierror.New(apierror.KindInternal, "logger not available in context").WithRetryable(apierror.False)
	ErrBootConfigNotAvailable     = apierror.New(apierror.KindInternal, "boot config not available in context").WithRetryable(apierror.False)
	ErrHTTPMiddlewareNotAvailable = apierror.New(apierror.KindInternal, "HTTP middleware registry not available in context").WithRetryable(apierror.False)
)

func NewOTELInitError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to initialize OTEL provider").WithCause(cause)
}
