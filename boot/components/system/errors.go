package system

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable               = apierror.New(apierror.KindInternal, "logger not available").WithRetryable(apierror.False)
	ErrEventBusNotAvailable             = apierror.New(apierror.KindInternal, "event bus not available").WithRetryable(apierror.False)
	ErrRegistryNotAvailable             = apierror.New(apierror.KindInternal, "registry not available").WithRetryable(apierror.False)
	ErrRelayNotAvailable                = apierror.New(apierror.KindInternal, "relay node not available").WithRetryable(apierror.False)
	ErrTranscoderNotAvailable           = apierror.New(apierror.KindInternal, "transcoder not available").WithRetryable(apierror.False)
	ErrFunctionRegistryNotAvailable     = apierror.New(apierror.KindInternal, "function registry not available in context").WithRetryable(apierror.False)
	ErrEventBusNotAvailableForCluster   = apierror.New(apierror.KindInternal, "event bus not available for cluster").WithRetryable(apierror.False)
	ErrTranscoderNotAvailableForCluster = apierror.New(apierror.KindInternal, "transcoder not available for cluster").WithRetryable(apierror.False)
	ErrRelayNotAvailableForCluster      = apierror.New(apierror.KindInternal, "relay node not available for cluster").WithRetryable(apierror.False)
	ErrRouterNotAvailable               = apierror.New(apierror.KindInternal, "router not available in context").WithRetryable(apierror.False)
	ErrTopologyNotAvailable             = apierror.New(apierror.KindInternal, "topology not available in context").WithRetryable(apierror.False)
)

func NewHostnameError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to get hostname for cluster node name").WithCause(cause)
}

func NewFactoryStartError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start factory registry").WithCause(cause)
}

func NewConnectionManagerPreStartError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to pre-start connection manager").WithCause(cause)
}

func NewConnectionManagerStopError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to stop connection manager after port allocation").WithCause(cause)
}

func NewMembershipStartError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start membership service").WithCause(cause)
}

func NewInternodeStartError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start internode service").WithCause(cause)
}
