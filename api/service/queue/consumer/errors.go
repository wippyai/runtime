package consumer

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrQueueIDRequired    = apierror.New(apierror.Invalid, "queue ID is required").WithRetryable(apierror.False)
	ErrFunctionIDRequired = apierror.New(apierror.Invalid, "function ID is required").WithRetryable(apierror.False)
)

func NewConcurrencyExceededError(value, maxVal int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("concurrency %d exceeds maximum %d", value, maxVal)).WithRetryable(apierror.False)
}

func NewPrefetchExceededError(value, maxVal int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("prefetch %d exceeds maximum %d", value, maxVal)).WithRetryable(apierror.False)
}
