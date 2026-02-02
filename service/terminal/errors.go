package terminal

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrHostNotRunning     = apierror.New(apierror.Unavailable, "host is not running").WithRetryable(apierror.False)
	ErrHostShuttingDown   = apierror.New(apierror.Unavailable, "host is shutting down").WithRetryable(apierror.False)
	ErrHostAlreadyRunning = apierror.New(apierror.Conflict, "host already running").WithRetryable(apierror.False)
)

func NewDecodeConfigError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Invalid, "failed to decode terminal config").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}
