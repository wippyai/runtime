package event

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for event bus errors.
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

// NewRouterCanceledError creates an error when router context is canceled.
func NewRouterCanceledError(err error) *Error {
	return &Error{
		kind:      apierror.KindCanceled,
		message:   "router context canceled: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewSubscriberError creates an error for subscriber creation failures.
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewAwaitTimeoutError creates an error for event await timeout.
func NewAwaitTimeoutError(path Path) *Error {
	return &Error{
		kind:      apierror.KindTimeout,
		message:   "await timeout waiting for event: " + string(path),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"path": string(path)}),
	}
}
