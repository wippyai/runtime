package memory

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrGCIntervalInvalid = apierror.New(apierror.KindInvalid, "gc_interval must be greater than 0").WithRetryable(apierror.False)
)

func NewInvalidGCIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid gc_interval duration format").WithCause(cause).WithRetryable(apierror.False)
}

var (
	ErrInvalidMaxSize = apierror.New(apierror.KindInvalid, "max_size must be greater than 0").WithRetryable(apierror.False)

	ErrInvalidCleanupInterval = apierror.New(apierror.KindInvalid, "cleanup_interval must be greater than 0").WithRetryable(apierror.False)
)

func NewInvalidDurationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid duration format").WithCause(cause).WithRetryable(apierror.False)
}
