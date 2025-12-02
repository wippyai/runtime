package interceptor

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for function interceptor errors
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

// NewInterceptorExistsError creates an error when interceptor already exists
func NewInterceptorExistsError(name string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "interceptor \"" + name + "\" already registered",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"name": name}),
	}
}

// NewInterceptorNotFoundError creates an error when interceptor not found
func NewInterceptorNotFoundError(name string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "interceptor \"" + name + "\" not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"name": name}),
	}
}
