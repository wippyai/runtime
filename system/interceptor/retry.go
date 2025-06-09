package interceptor

import (
	"context"
	"fmt"

	"github.com/davecgh/go-spew/spew"

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
func (i *RetryInterceptor) Handle(ctx context.Context, next func() *runtime.Result) *runtime.Result {
	attempt := 0

	fmt.Println("RetryInterceptor")

	options := apiinterceptor.GetOptionsFromContext(ctx)

	// Use configured max attempts or fallback to default
	maxAttempts := options.Retry.MaxAttempts

	// If max attempts is 0, skip retry
	if maxAttempts == 0 {
		fmt.Println("Retry Interceptor skipped")
		return next()
	}

	for {
		select {
		case <-ctx.Done():
			return &runtime.Result{Error: ctx.Err()}
		default:
			result := next()

			// If no error and no retryable status, return success
			if result == nil || result.Error == nil {
				fmt.Println("Retry Interceptor completed")
				return result
			}

			spew.Dump("retrying")

			attempt++
			if attempt >= maxAttempts {
				fmt.Println("Retry Interceptor completed")
				return result
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
