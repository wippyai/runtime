package consumer

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrQueueIDRequired    = apierror.New(apierror.KindInvalid, "queue ID is required").WithRetryable(apierror.False)
	ErrFunctionIDRequired = apierror.New(apierror.KindInvalid, "function ID is required").WithRetryable(apierror.False)
)

func NewConcurrencyExceededError(value, max int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("concurrency %d exceeds maximum %d", value, max)).WithRetryable(apierror.False)
}

func NewPrefetchExceededError(value, max int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("prefetch %d exceeds maximum %d", value, max)).WithRetryable(apierror.False)
}
