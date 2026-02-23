// SPDX-License-Identifier: MPL-2.0

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

func NewValidationError(registry string, cause error) apierror.Error {
	msg := "token validation failed"
	if registry != "" {
		msg = msg + " for " + registry
	}
	return apierror.New(apierror.PermissionDenied, msg).WithCause(cause).WithRetryable(apierror.False)
}

func NewStoreError(registry string, cause error) apierror.Error {
	msg := "failed to store credentials"
	if registry != "" {
		msg = msg + " for " + registry
	}
	return apierror.New(apierror.Internal, msg).WithCause(cause).WithRetryable(apierror.False)
}

func NewRemoveError(registry string, cause error) apierror.Error {
	msg := "failed to remove credentials"
	if registry != "" {
		msg = msg + " for " + registry
	}
	return apierror.New(apierror.Internal, msg).WithCause(cause).WithRetryable(apierror.False)
}

func NewClientError(registry string, cause error) apierror.Error {
	msg := "failed to create auth client"
	if registry != "" {
		msg = msg + " for " + registry
	}
	return apierror.New(apierror.Invalid, msg).WithCause(cause).WithRetryable(apierror.False)
}
