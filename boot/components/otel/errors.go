package otel

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable         = apierror.New(apierror.Internal, "logger not available in context").WithRetryable(apierror.False)
	ErrBootConfigNotAvailable     = apierror.New(apierror.Internal, "boot config not available in context").WithRetryable(apierror.False)
	ErrHTTPMiddlewareNotAvailable = apierror.New(apierror.Internal, "HTTP middleware registry not available in context").WithRetryable(apierror.False)
)

func NewOTELInitError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to initialize OTEL provider").WithCause(cause)
}
