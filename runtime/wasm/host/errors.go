package host

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for host registry errors
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

// NewHostAlreadyRegisteredError creates an error when host is already registered
func NewHostAlreadyRegisteredError(namespace string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "host \"" + namespace + "\" already registered",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"namespace": namespace}),
	}
}

// NewInstantiateHostError creates an error for host instantiation failure
func NewInstantiateHostError(namespace string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "instantiate host " + namespace + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"namespace": namespace, "cause": err.Error()}),
		cause:     err,
	}
}

// Sentinel errors

// ErrEmptyNamespace is returned when host namespace is empty
var ErrEmptyNamespace = &Error{
	kind:      apierror.KindInvalid,
	message:   "host namespace cannot be empty",
	retryable: apierror.False,
}
