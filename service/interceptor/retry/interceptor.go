package retry

import (
	"context"
	"errors"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
	"github.com/wippyai/runtime/api/runtime"
	retryapi "github.com/wippyai/runtime/api/service/interceptor/retry"
)

// Interceptor implements retry functionality
type Interceptor struct {
}

// New creates a new retry interceptor
func New() *Interceptor {
	return &Interceptor{}
}

// Handle implements the interceptor interface
func (i *Interceptor) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	opts, ok := apiinterceptor.GetOptions(ctx)
	if !ok {
		return next(ctx)
	}

	val, ok := opts.Get("retry")
	if !ok {
		return next(ctx)
	}

	retryOpts, ok := val.(*retryapi.Options)
	if !ok {
		return next(ctx)
	}

	maxAttempts := retryOpts.MaxAttempts
	if maxAttempts == 0 {
		return next(ctx)
	}

	backoffMs := retryOpts.BackoffMs
	if backoffMs == 0 {
		backoffMs = 100
	}
	backoff := time.Duration(backoffMs) * time.Millisecond

	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return &runtime.Result{Error: ctx.Err()}, ctx
		default:
			result, newCtx := next(ctx)

			if result == nil || result.Error == nil {
				return result, newCtx
			}

			// Check if error is retryable
			if !i.isRetryable(result.Error) {
				return result, newCtx
			}

			attempt++
			if attempt >= maxAttempts {
				return result, newCtx
			}

			// Exponential backoff with context cancellation check
			delay := backoff * time.Duration(attempt)
			select {
			case <-ctx.Done():
				return &runtime.Result{Error: ctx.Err()}, ctx
			case <-time.After(delay):
				continue
			}
		}
	}
}

// isRetryable checks if an error should be retried
func (i *Interceptor) isRetryable(err error) bool {
	var apiErr apierror.Error
	if !errors.As(err, &apiErr) {
		return true
	}

	// Check Retryable flag first
	if apiErr.Retryable() == apierror.False {
		return false
	}

	// Check error kind - skip non-retryable errors
	kind := apiErr.Kind()
	switch kind {
	case apierror.KindInvalid,
		apierror.KindPermissionDenied,
		apierror.KindInternal:
		return false
	default:
		return true
	}
}
