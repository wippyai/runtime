package security

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewSubscriberError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}
