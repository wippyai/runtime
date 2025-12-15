package service

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable             = apierror.New(apierror.KindInternal, "logger not available").WithRetryable(apierror.False)
	ErrEventBusNotAvailable           = apierror.New(apierror.KindInternal, "event bus not available").WithRetryable(apierror.False)
	ErrTranscoderNotAvailable         = apierror.New(apierror.KindInternal, "transcoder not available").WithRetryable(apierror.False)
	ErrHandlerRegistryNotAvailable    = apierror.New(apierror.KindInternal, "handler registry not available").WithRetryable(apierror.False)
	ErrProcessFactoryNotAvailable     = apierror.New(apierror.KindInternal, "process factory not available").WithRetryable(apierror.False)
	ErrDispatcherRegistryNotAvailable = apierror.New(apierror.KindInternal, "dispatcher registry not available").WithRetryable(apierror.False)
	ErrRegistryNotAvailable           = apierror.New(apierror.KindInternal, "registry not available in context").WithRetryable(apierror.False)
	ErrFunctionRegistryNotAvailable   = apierror.New(apierror.KindInternal, "function registry not available in context").WithRetryable(apierror.False)
	ErrFilesystemRegistryNotAvailable = apierror.New(apierror.KindInternal, "filesystem registry not available in context").WithRetryable(apierror.False)
	ErrPIDGeneratorNotAvailable       = apierror.New(apierror.KindInternal, "pid generator not available in context").WithRetryable(apierror.False)
	ErrRelayNotAvailable              = apierror.New(apierror.KindInternal, "relay node not available").WithRetryable(apierror.False)
	ErrTopologyNotAvailable           = apierror.New(apierror.KindInternal, "topology not available in context").WithRetryable(apierror.False)
	ErrProcessManagerNotAvailable     = apierror.New(apierror.KindInternal, "process manager not available in context").WithRetryable(apierror.False)
)

func NewEndpointFactoryError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create endpoint factory").WithCause(cause)
}

func NewStaticFactoryError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create static factory").WithCause(cause)
}

func NewHTTPManagerError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create http manager").WithCause(cause)
}

func NewSQLManagerError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create sql manager").WithCause(cause)
}
