package system

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var (
	ErrLoggerNotAvailable               = &Error{kind: apierror.KindInternal, message: "logger not available"}
	ErrEventBusNotAvailable             = &Error{kind: apierror.KindInternal, message: "event bus not available"}
	ErrRegistryNotAvailable             = &Error{kind: apierror.KindInternal, message: "registry not available"}
	ErrRelayNotAvailable                = &Error{kind: apierror.KindInternal, message: "relay node not available"}
	ErrTranscoderNotAvailable           = &Error{kind: apierror.KindInternal, message: "transcoder not available"}
	ErrFunctionRegistryNotAvailable     = &Error{kind: apierror.KindInternal, message: "function registry not available in context"}
	ErrEventBusNotAvailableForCluster   = &Error{kind: apierror.KindInternal, message: "event bus not available for cluster"}
	ErrTranscoderNotAvailableForCluster = &Error{kind: apierror.KindInternal, message: "transcoder not available for cluster"}
	ErrRelayNotAvailableForCluster      = &Error{kind: apierror.KindInternal, message: "relay node not available for cluster"}
	ErrRouterNotAvailable               = &Error{kind: apierror.KindInternal, message: "router not available in context"}
	ErrTopologyNotAvailable             = &Error{kind: apierror.KindInternal, message: "topology not available in context"}
)

func NewHostnameError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to get hostname for cluster node name",
		cause:   cause,
	}
}

func NewFactoryStartError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to start factory registry",
		cause:   cause,
	}
}

func NewConnectionManagerPreStartError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to pre-start connection manager",
		cause:   cause,
	}
}

func NewConnectionManagerStopError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to stop connection manager after port allocation",
		cause:   cause,
	}
}

func NewMembershipStartError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to start membership service",
		cause:   cause,
	}
}

func NewInternodeStartError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to start internode service",
		cause:   cause,
	}
}
