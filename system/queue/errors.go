package queue

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error implements apierror.Error for queue manager errors
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

// NewSubscriberError creates an error for subscriber creation failure
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create queue event subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewDriverNotFoundError creates an error when driver not found
func NewDriverNotFoundError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "driver not found: " + id.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"driver_id": id.String()}),
	}
}

// NewDeclareQueueError creates an error when queue declaration fails
func NewDeclareQueueError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to declare queue: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewInvalidQueueTypeError creates an error for invalid queue type
func NewInvalidQueueTypeError(q registry.ID, actualType string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "queue has invalid type: " + actualType,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"queue": q.String(), "type": actualType}),
	}
}

// NewInvalidDriverTypeError creates an error for invalid driver type
func NewInvalidDriverTypeError(id registry.ID, actualType string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "driver has invalid type: " + actualType,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"driver_id": id.String(), "type": actualType}),
	}
}

// Sentinel errors

// ErrNoPublishFunc is returned when no publish function is configured
var ErrNoPublishFunc = &Error{
	kind:      apierror.KindUnavailable,
	message:   "no publish function configured",
	retryable: apierror.False,
}
