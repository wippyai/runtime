package function

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors for function operations.
var (
	ErrRegistryNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "function registry not found in context",
		retryable: apierror.False,
	}

	ErrProcessContextNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "process context not found",
		retryable: apierror.False,
	}

	ErrCallNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "async call not found",
		retryable: apierror.False,
	}

	ErrCallCancelled = &Error{
		kind:      apierror.KindCanceled,
		message:   "async call cancelled",
		retryable: apierror.False,
	}

	ErrNilContext = &Error{
		kind:      apierror.KindInvalid,
		message:   "nil context",
		retryable: apierror.False,
	}

	ErrNilCallback = &Error{
		kind:      apierror.KindInvalid,
		message:   "nil callback",
		retryable: apierror.False,
	}

	ErrNodeNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "relay node not configured",
		retryable: apierror.False,
	}

	ErrPIDNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "frame PID not found in context",
		retryable: apierror.False,
	}

	ErrPIDGeneratorNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "PID generator not found in context",
		retryable: apierror.False,
	}
)

// Error represents a function error with metadata.
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

// WithCause returns a new error with the given cause.
func (e *Error) WithCause(cause error) *Error {
	return &Error{
		kind:      e.kind,
		message:   e.message,
		retryable: e.retryable,
		details:   e.details,
		cause:     cause,
	}
}

// WithDetails returns a new error with the given details.
func (e *Error) WithDetails(details attrs.Attributes) *Error {
	return &Error{
		kind:      e.kind,
		message:   e.message,
		retryable: e.retryable,
		details:   details,
		cause:     e.cause,
	}
}

// WithMessage returns a new error with a custom message.
func (e *Error) WithMessage(msg string) *Error {
	return &Error{
		kind:      e.kind,
		message:   msg,
		retryable: e.retryable,
		details:   e.details,
		cause:     e.cause,
	}
}

// NewHandlerNotFoundError creates an error for missing handler.
func NewHandlerNotFoundError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no handler registered for target: " + id.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"target": id.String()}),
	}
}

// NewInvalidHandlerError creates an error for invalid handler type.
func NewInvalidHandlerError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "invalid handler type for target: " + id.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"target": id.String()}),
	}
}

// NewFrameContextError creates an error for frame context failures.
func NewFrameContextError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to set frame context: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewSubscriberError creates an error for event subscriber failures.
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewInterceptorExistsError creates an error when interceptor already exists.
func NewInterceptorExistsError(name string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "interceptor \"" + name + "\" already registered",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"name": name}),
	}
}

// NewInterceptorNotFoundError creates an error when interceptor not found.
func NewInterceptorNotFoundError(name string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "interceptor \"" + name + "\" not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"name": name}),
	}
}

// NewInterceptorSealedError creates an error when registry is sealed.
func NewInterceptorSealedError() *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "interceptor registry is sealed",
		retryable: apierror.False,
	}
}
