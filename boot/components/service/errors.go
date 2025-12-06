package service

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
	ErrLoggerNotAvailable             = &Error{kind: apierror.KindInternal, message: "logger not available"}
	ErrEventBusNotAvailable           = &Error{kind: apierror.KindInternal, message: "event bus not available"}
	ErrTranscoderNotAvailable         = &Error{kind: apierror.KindInternal, message: "transcoder not available"}
	ErrHandlerRegistryNotAvailable    = &Error{kind: apierror.KindInternal, message: "handler registry not available"}
	ErrProcessFactoryNotAvailable     = &Error{kind: apierror.KindInternal, message: "process factory not available"}
	ErrDispatcherRegistryNotAvailable = &Error{kind: apierror.KindInternal, message: "dispatcher registry not available"}
	ErrRegistryNotAvailable           = &Error{kind: apierror.KindInternal, message: "registry not available in context"}
	ErrFunctionRegistryNotAvailable   = &Error{kind: apierror.KindInternal, message: "function registry not available in context"}
	ErrFilesystemRegistryNotAvailable = &Error{kind: apierror.KindInternal, message: "filesystem registry not available in context"}
	ErrPIDGeneratorNotAvailable       = &Error{kind: apierror.KindInternal, message: "pid generator not available in context"}
)

func NewEndpointFactoryError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create endpoint factory",
		cause:   cause,
	}
}

func NewStaticFactoryError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create static factory",
		cause:   cause,
	}
}

func NewHTTPManagerError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create http manager",
		cause:   cause,
	}
}

func NewSQLManagerError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create sql manager",
		cause:   cause,
	}
}
