// SPDX-License-Identifier: MPL-2.0

package resource

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for resource operations.
var (
	ErrLocked = apierror.New(apierror.Unavailable, "resource is locked").WithRetryable(apierror.True)
	ErrClosed = apierror.New(apierror.Unavailable, "resource provider is closed").WithRetryable(apierror.False)
)

// NewSubscriberError creates an error for subscriber creation failure.
func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}
