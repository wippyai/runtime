package eventbus

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
)

// NewSubscriberError creates an error for subscriber creation failures.
func NewSubscriberError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to create subscriber: "+err.Error(),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewRouterCanceledError creates an error when router context is canceled.
func NewRouterCanceledError(err error) apierror.Error {
	return apierror.E(
		apierror.Canceled,
		"router context canceled: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewAwaitTimeoutError creates an error for event await timeout.
func NewAwaitTimeoutError(path event.Path) apierror.Error {
	return apierror.E(
		apierror.Timeout,
		"await timeout waiting for event: "+string(path),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"path": path}),
		nil,
	)
}
