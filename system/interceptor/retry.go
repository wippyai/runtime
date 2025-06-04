package interceptor

import (
	"context"
	"fmt"
	"time"

	"github.com/davecgh/go-spew/spew"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
)

// RetryInterceptor implements retry functionality
type RetryInterceptor struct {
	policy *apiinterceptor.RetryPolicy
}

// NewRetryInterceptor creates a new retry interceptor with the given policy
func NewRetryInterceptor(policy *apiinterceptor.RetryPolicy) *RetryInterceptor {
	return &RetryInterceptor{
		policy: policy,
	}
}

// Handle implements the interceptor interface
func (i *RetryInterceptor) Handle(ctx context.Context, next func() *runtime.Result, opts ...apiinterceptor.Option) *runtime.Result {
	attempt := 0

	fmt.Println("RetryInterceptor")

	// Create config and apply options
	config := &apiinterceptor.Config{}
	for _, opt := range opts {
		opt(config)
	}

	for {
		select {
		case <-ctx.Done():
			return &runtime.Result{Error: ctx.Err()}
		default:
			result := next()

			// If no error and no retryable status, return success
			if result == nil || result.Error == nil {
				return result
			}

			spew.Dump("retrying")

			attempt++
			if attempt >= i.policy.MaxAttempts {
				return result
			}

			interval := i.policy.InitialInterval
			for j := 0; j < attempt-1; j++ {
				interval = time.Duration(float64(interval) * i.policy.Multiplier)
				if interval > i.policy.MaxInterval {
					interval = i.policy.MaxInterval
					break
				}
			}

			select {
			case <-ctx.Done():
				return &runtime.Result{Error: ctx.Err()}
			case <-time.After(interval):
				continue
			}
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
