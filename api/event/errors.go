package event

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// NewRouterCanceledError creates an error when router context is canceled.
func NewRouterCanceledError(err error) apierror.Error {
	return apierror.E(
		apierror.KindCanceled,
		"router context canceled: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewAwaitTimeoutError creates an error for event await timeout.
func NewAwaitTimeoutError(path Path) apierror.Error {
	return apierror.E(
		apierror.KindTimeout,
		"await timeout waiting for event: "+string(path),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"path": string(path)}),
		nil,
	)
}
