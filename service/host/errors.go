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
	ErrInvalidWorkers = apierror.New(apierror.KindInvalid, "workers must be greater than 0").WithRetryable(apierror.False)

	ErrInvalidQueueSize = apierror.New(apierror.KindInvalid, "queue_size must be greater than 0").WithRetryable(apierror.False)

	ErrInvalidLocalQueueSize = apierror.New(apierror.KindInvalid, "local_queue_size must be greater than 0").WithRetryable(apierror.False)
)

func NewDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode host config").WithCause(cause).WithRetryable(apierror.False)
}
