package host

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrInvalidWorkers        = apierror.New(apierror.Invalid, "workers must be greater than 0").WithRetryable(apierror.False)
	ErrInvalidQueueSize      = apierror.New(apierror.Invalid, "queue size must be greater than 0").WithRetryable(apierror.False)
	ErrInvalidLocalQueueSize = apierror.New(apierror.Invalid, "local queue size must be greater than 0").WithRetryable(apierror.False)
)
