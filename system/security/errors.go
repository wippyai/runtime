// SPDX-License-Identifier: MPL-2.0

package security

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrRegistryStopped      = apierror.New(apierror.Unavailable, "security policy registry is not running").WithRetryable(apierror.True)
	ErrRegistryStarted      = apierror.New(apierror.Invalid, "security policy registry is already running").WithRetryable(apierror.False)
	ErrInvalidPolicyPayload = apierror.New(apierror.Invalid, "invalid policy payload").WithRetryable(apierror.False)
)

func NewSubscriberError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}
