package memory

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrInvalidMaxSize         = apierror.New(apierror.KindInvalid, "max size must be non-negative").WithRetryable(apierror.False)
	ErrInvalidCleanupInterval = apierror.New(apierror.KindInvalid, "cleanup interval must be non-negative").WithRetryable(apierror.False)
)

func NewInvalidDurationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid duration format").WithCause(cause).WithRetryable(apierror.False)
}
