package security

import (
	apierror "github.com/wippyai/runtime/api/error"
)

func NewSubscriberError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber").WithCause(cause).WithRetryable(apierror.False)
}
