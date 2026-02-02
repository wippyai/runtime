package auth

import (
	apierror "github.com/wippyai/runtime/api/error"
)

func NewTokenReadError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read token").WithCause(cause).WithRetryable(apierror.False)
}

func NewTokenEmptyError() apierror.Error {
	return apierror.New(apierror.Invalid, "token cannot be empty").WithRetryable(apierror.False)
}

func NewTokenInvalidError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid token format").WithCause(cause).WithRetryable(apierror.False)
}

func NewValidationError(cause error) apierror.Error {
	return apierror.New(apierror.PermissionDenied, "token validation failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewStoreError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to store credentials").WithCause(cause).WithRetryable(apierror.False)
}

func NewRemoveError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to remove credentials").WithCause(cause).WithRetryable(apierror.False)
}

func NewClientError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to create auth client").WithCause(cause).WithRetryable(apierror.False)
}
