package interceptor

import (
	"context"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
)

// RetryInterceptor implements retry functionality
type RetryInterceptor struct {
}

// NewRetryInterceptor creates a new retry interceptor with the given max attempts
func NewRetryInterceptor() *RetryInterceptor {
	return &RetryInterceptor{}
}

// Handle implements the interceptor interface
func (i *RetryInterceptor) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	attempt := 0

	options := apiinterceptor.GetOptionsFromContext(ctx)

	// Use configured max attempts or fallback to default
	maxAttempts := options.Retry.MaxAttempts

	// If max attempts is 0, skip retry
	if maxAttempts == 0 {
		return next(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return &runtime.Result{Error: ctx.Err()}, ctx
		default:
			result, newCtx := next(ctx)

			// If no error and no retryable status, return success
			if result == nil || result.Error == nil {
				return result, newCtx
			}

			attempt++
			if attempt >= maxAttempts {
				return result, newCtx
			}

			// Continue immediately to next attempt
			continue
		}
	}
}

// Format implements the payload.Payload interface
func (i *RetryInterceptor) Format() payload.Format {
	return payload.Golang
}

// Data implements the payload.Payload interface
func (i *RetryInterceptor) Data() any {
	return i
}
