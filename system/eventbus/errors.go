// SPDX-License-Identifier: MPL-2.0

package eventbus

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
)

var (
	ErrNilChannel            = errors.New("nil channel provided")
	ErrBusClosed             = errors.New("bus is closed")
	ErrSubscribersCapReached = errors.New("eventbus subscribers cap reached")
)

// NewSubscriberError creates an error for subscriber creation failures.
func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewRouterCanceledError creates an error when router context is canceled.
func NewRouterCanceledError(err error) apierror.Error {
	return apierror.New(apierror.Canceled, "router context canceled").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewAwaitTimeoutError creates an error for event await timeout.
func NewAwaitTimeoutError(path event.Path) apierror.Error {
	return apierror.New(apierror.Timeout, "await timeout waiting for event").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}
