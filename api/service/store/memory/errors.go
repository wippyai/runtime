package memory

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrInvalidMaxSize         = apierror.New(apierror.Invalid, "max size must be non-negative").WithRetryable(apierror.False)
	ErrInvalidCleanupInterval = apierror.New(apierror.Invalid, "cleanup interval must be non-negative").WithRetryable(apierror.False)
)

func NewInvalidDurationError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid duration format").WithCause(cause).WithRetryable(apierror.False)
}
