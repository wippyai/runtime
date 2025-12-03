package storage

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
	ErrLoggerNotAvailable           = &Error{kind: apierror.KindInternal, message: "logger not available in context"}
	ErrTranscoderNotAvailable       = &Error{kind: apierror.KindInternal, message: "transcoder not available in context"}
	ErrEventBusNotAvailable         = &Error{kind: apierror.KindInternal, message: "event bus not available in context"}
	ErrResourceRegistryNotAvailable = &Error{kind: apierror.KindInternal, message: "resource registry not available in context"}
	ErrSecurityRegistryNotAvailable = &Error{kind: apierror.KindInternal, message: "security registry not available in context"}
	ErrHandlerRegistryNotAvailable  = &Error{kind: apierror.KindInternal, message: "handler registry not available in context"}
	ErrRegistryNotAvailable         = &Error{kind: apierror.KindInternal, message: "registry not available in context"}
)

func NewSQLManagerError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create sql manager",
		cause:   cause,
	}
}
