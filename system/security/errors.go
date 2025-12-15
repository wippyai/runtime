package security

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrUnauthorized = apierror.New(apierror.KindPermissionDenied, "unauthorized").WithRetryable(apierror.False)

	ErrInvalidToken = apierror.New(apierror.KindInvalid, "invalid token").WithRetryable(apierror.False)

	ErrTokenExpired = apierror.New(apierror.KindPermissionDenied, "token expired").WithRetryable(apierror.False)
)

func NewAuthenticationError(cause error) apierror.Error {
	return apierror.New(apierror.KindPermissionDenied, "authentication failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewAuthorizationError(cause error) apierror.Error {
	return apierror.New(apierror.KindPermissionDenied, "authorization failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewSubscriberError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create subscriber").WithCause(cause).WithRetryable(apierror.False)
}
