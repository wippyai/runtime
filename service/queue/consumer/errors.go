package consumer

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewConcurrencyExceededError(value, maxVal int) apierror.Error {
	return apierror.New(apierror.Invalid, "concurrency exceeds maximum").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"concurrency": value, "max": maxVal}))
}

func NewPrefetchExceededError(value, maxVal int) apierror.Error {
	return apierror.New(apierror.Invalid, "prefetch exceeds maximum").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"prefetch": value, "max": maxVal}))
}
