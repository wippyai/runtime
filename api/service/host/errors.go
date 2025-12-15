package host

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrInvalidWorkers        = apierror.New(apierror.KindInvalid, "workers must be greater than 0").WithRetryable(apierror.False)
	ErrInvalidQueueSize      = apierror.New(apierror.KindInvalid, "queue size must be greater than 0").WithRetryable(apierror.False)
	ErrInvalidLocalQueueSize = apierror.New(apierror.KindInvalid, "local queue size must be greater than 0").WithRetryable(apierror.False)
)
