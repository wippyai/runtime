package host

import (
	"errors"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrHostNotRunning     = errors.New("host is not running")
	ErrHostShuttingDown   = errors.New("host is shutting down")
	ErrHostAlreadyRunning = errors.New("host already running")
)

var (
	ErrInvalidWorkers = apierror.New(apierror.Invalid, "workers must be greater than 0").WithRetryable(apierror.False)

	ErrInvalidQueueSize = apierror.New(apierror.Invalid, "queue_size must be greater than 0").WithRetryable(apierror.False)

	ErrInvalidLocalQueueSize = apierror.New(apierror.Invalid, "local_queue_size must be greater than 0").WithRetryable(apierror.False)
)

func NewDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode host config").WithCause(cause).WithRetryable(apierror.False)
}
