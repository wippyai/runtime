package consumer

import (
	queueapi "github.com/wippyai/runtime/api/queue"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrQueueIDRequired    = queueapi.ErrQueueIDRequired
	ErrFunctionIDRequired = queueapi.ErrFunctionIDRequired
)

func NewConcurrencyExceededError(concurrency, maxVal int) apierror.Error {
	return queueapi.NewConcurrencyExceededError(concurrency, maxVal)
}

func NewPrefetchExceededError(prefetch, maxVal int) apierror.Error {
	return queueapi.NewPrefetchExceededError(prefetch, maxVal)
}
