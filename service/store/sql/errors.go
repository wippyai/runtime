package sql

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrInvalidResourceType = apierror.New(apierror.Invalid, "acquired resource is not a valid database connection").WithRetryable(apierror.False)
)

func NewInvalidCleanupIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid cleanup interval duration").WithCause(cause).WithRetryable(apierror.False)
}
