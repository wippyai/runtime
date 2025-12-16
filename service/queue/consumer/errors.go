package consumer

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

func NewConcurrencyExceededError(value, maxVal int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("concurrency %d exceeds maximum %d", value, maxVal)).WithRetryable(apierror.False)
}

func NewPrefetchExceededError(value, maxVal int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("prefetch %d exceeds maximum %d", value, maxVal)).WithRetryable(apierror.False)
}
