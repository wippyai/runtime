package service

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable             = apierror.New(apierror.Internal, "logger not available").WithRetryable(apierror.False)
	ErrEventBusNotAvailable           = apierror.New(apierror.Internal, "event bus not available").WithRetryable(apierror.False)
	ErrTranscoderNotAvailable         = apierror.New(apierror.Internal, "transcoder not available").WithRetryable(apierror.False)
	ErrHandlerRegistryNotAvailable    = apierror.New(apierror.Internal, "handler registry not available").WithRetryable(apierror.False)
	ErrProcessFactoryNotAvailable     = apierror.New(apierror.Internal, "process factory not available").WithRetryable(apierror.False)
	ErrDispatcherRegistryNotAvailable = apierror.New(apierror.Internal, "dispatcher registry not available").WithRetryable(apierror.False)
	ErrRegistryNotAvailable           = apierror.New(apierror.Internal, "registry not available in context").WithRetryable(apierror.False)
	ErrFunctionRegistryNotAvailable   = apierror.New(apierror.Internal, "function registry not available in context").WithRetryable(apierror.False)
	ErrFilesystemRegistryNotAvailable = apierror.New(apierror.Internal, "filesystem registry not available in context").WithRetryable(apierror.False)
	ErrPIDGeneratorNotAvailable       = apierror.New(apierror.Internal, "pid generator not available in context").WithRetryable(apierror.False)
	ErrRelayNotAvailable              = apierror.New(apierror.Internal, "relay node not available").WithRetryable(apierror.False)
	ErrTopologyNotAvailable           = apierror.New(apierror.Internal, "topology not available in context").WithRetryable(apierror.False)
	ErrProcessManagerNotAvailable     = apierror.New(apierror.Internal, "process manager not available in context").WithRetryable(apierror.False)
)

func NewEndpointFactoryError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create endpoint factory").WithCause(cause)
}

func NewStaticFactoryError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create static factory").WithCause(cause)
}

func NewHTTPManagerError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create http manager").WithCause(cause)
}
