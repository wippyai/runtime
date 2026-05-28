// SPDX-License-Identifier: MPL-2.0

package system

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable               = apierror.New(apierror.Internal, "logger not available").WithRetryable(apierror.False)
	ErrEventBusNotAvailable             = apierror.New(apierror.Internal, "event bus not available").WithRetryable(apierror.False)
	ErrRegistryNotAvailable             = apierror.New(apierror.Internal, "registry not available").WithRetryable(apierror.False)
	ErrRelayNotAvailable                = apierror.New(apierror.Internal, "relay node not available").WithRetryable(apierror.False)
	ErrFunctionRegistryNotAvailable     = apierror.New(apierror.Internal, "function registry not available in context").WithRetryable(apierror.False)
	ErrEventBusNotAvailableForCluster   = apierror.New(apierror.Internal, "event bus not available for cluster").WithRetryable(apierror.False)
	ErrTranscoderNotAvailableForCluster = apierror.New(apierror.Internal, "transcoder not available for cluster").WithRetryable(apierror.False)
	ErrRelayNotAvailableForCluster      = apierror.New(apierror.Internal, "relay node not available for cluster").WithRetryable(apierror.False)
	ErrRouterNotAvailable               = apierror.New(apierror.Internal, "router not available in context").WithRetryable(apierror.False)
	ErrTopologyNotAvailable             = apierror.New(apierror.Internal, "topology not available in context").WithRetryable(apierror.False)
	ErrHandlerRegistryNotAvailable      = apierror.New(apierror.Internal, "handler registry not available in context").WithRetryable(apierror.False)
)

func NewHostnameError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get hostname for cluster node name").WithCause(cause)
}

func NewFactoryStartError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start factory registry").WithCause(cause)
}

func NewConnectionManagerPreStartError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to pre-start connection manager").WithCause(cause)
}

func NewConnectionManagerStopError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to stop connection manager after port allocation").WithCause(cause)
}

func NewMembershipStartError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start membership service").WithCause(cause)
}

func NewInternodeStartError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start internode service").WithCause(cause)
}
