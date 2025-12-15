package resource

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for resource operations.
var (
	ErrLocked = apierror.New(apierror.KindUnavailable, "resource is locked").WithRetryable(apierror.True)
	ErrClosed = apierror.New(apierror.KindUnavailable, "resource provider is closed").WithRetryable(apierror.False)
	ErrInUse  = apierror.New(apierror.KindUnavailable, "resource is in use").WithRetryable(apierror.True)
)

// NewSubscriberError creates an error for subscriber creation failure.
func NewSubscriberError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to create subscriber: "+err.Error(),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}
