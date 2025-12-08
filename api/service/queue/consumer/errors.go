package consumer

import (
	queueapi "github.com/wippyai/runtime/api/queue"
)

var (
	ErrQueueIDRequired    = queueapi.ErrQueueIDRequired
	ErrFunctionIDRequired = queueapi.ErrFunctionIDRequired
)

func NewConcurrencyExceededError(concurrency, maxVal int) *queueapi.Error {
	return queueapi.NewConcurrencyExceededError(concurrency, maxVal)
}

func NewPrefetchExceededError(prefetch, maxVal int) *queueapi.Error {
	return queueapi.NewPrefetchExceededError(prefetch, maxVal)
}
