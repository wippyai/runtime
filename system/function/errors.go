package function

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error implements apierror.Error for function errors
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }

// Sentinel errors
var (
	ErrRegistryNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "function registry not found in context",
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
)

// NewHandlerNotFoundError creates an error for missing handler
func NewHandlerNotFoundError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no handler registered for target: " + id.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"target": id.String()}),
	}
}

// NewInvalidHandlerError creates an error for invalid handler type
func NewInvalidHandlerError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "invalid handler type for target: " + id.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"target": id.String()}),
	}
}

// NewFrameContextError creates an error for frame context failures
func NewFrameContextError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to set frame context: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
	}
}

// NewSubscriberError creates an error for event subscriber failures
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
	}
}
