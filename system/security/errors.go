package security

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrUnauthorized = apierror.New(apierror.PermissionDenied, "unauthorized").WithRetryable(apierror.False)

	ErrInvalidToken = apierror.New(apierror.Invalid, "invalid token").WithRetryable(apierror.False)

	ErrTokenExpired = apierror.New(apierror.PermissionDenied, "token expired").WithRetryable(apierror.False)
)

func NewAuthenticationError(cause error) apierror.Error {
	return apierror.New(apierror.PermissionDenied, "authentication failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewAuthorizationError(cause error) apierror.Error {
	return apierror.New(apierror.PermissionDenied, "authorization failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewSubscriberError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber").WithCause(cause).WithRetryable(apierror.False)
}
