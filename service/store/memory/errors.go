package memory

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrGCIntervalInvalid = apierror.New(apierror.Invalid, "gc_interval must be greater than 0").WithRetryable(apierror.False)
)

func NewInvalidGCIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid gc_interval duration format").WithCause(cause).WithRetryable(apierror.False)
}

var (
	ErrInvalidMaxSize = apierror.New(apierror.Invalid, "max_size must be greater than 0").WithRetryable(apierror.False)

	ErrInvalidCleanupInterval = apierror.New(apierror.Invalid, "cleanup_interval must be greater than 0").WithRetryable(apierror.False)
)

func NewInvalidDurationError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid duration format").WithCause(cause).WithRetryable(apierror.False)
}
