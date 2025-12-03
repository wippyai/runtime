package process

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

func NewNotAllowedError(action, target string) *Error {
	return &Error{
		kind:      apierror.KindPermissionDenied,
		message:   "not allowed to " + action + ": " + target,
		retryable: apierror.False,
	}
}

var ErrCouldNotAccessRegistry = &Error{
	kind:      apierror.KindInternal,
	message:   "could not access registry",
	retryable: apierror.False,
}

func NewCouldNotResolveError(pidOrName string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "could not resolve '" + pidOrName + "' as PID or registered name",
		retryable: apierror.False,
	}
}
