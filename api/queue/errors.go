package queue

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for queue operations.
var (
	ErrDriverNotFound     = apierror.New(apierror.NotFound, "queue driver not found").WithRetryable(apierror.False)
	ErrQueueNotFound      = apierror.New(apierror.NotFound, "queue not found").WithRetryable(apierror.False)
	ErrMessageExpired     = apierror.New(apierror.Invalid, "message expired").WithRetryable(apierror.False)
	ErrDriverIDRequired   = apierror.New(apierror.Invalid, "driver ID is required").WithRetryable(apierror.False)
	ErrQueueIDRequired    = apierror.New(apierror.Invalid, "queue ID is required").WithRetryable(apierror.False)
	ErrFunctionIDRequired = apierror.New(apierror.Invalid, "function ID is required").WithRetryable(apierror.False)
)
