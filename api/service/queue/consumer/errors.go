package consumer

import (
	queueapi "github.com/wippyai/runtime/api/queue"
)

var (
	ErrQueueIDRequired    = queueapi.ErrQueueIDRequired
	ErrFunctionIDRequired = queueapi.ErrFunctionIDRequired
)

func NewConcurrencyExceededError(concurrency, max int) *queueapi.Error {
	return queueapi.NewConcurrencyExceededError(concurrency, max)
}

func NewPrefetchExceededError(prefetch, max int) *queueapi.Error {
	return queueapi.NewPrefetchExceededError(prefetch, max)
}
