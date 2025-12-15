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

func NewDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode host config").WithCause(cause).WithRetryable(apierror.False)
}
