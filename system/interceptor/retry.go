package interceptor

import (
	"context"
	"fmt"
	"time"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
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
func (i *RetryInterceptor) Handle(ctx context.Context, next func() error, opts ...apiinterceptor.Option) error {
	var err error
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
			return ctx.Err()
		default:
			err = next()
			// If no error, return success
			if err == nil {
				return nil
			}

			attempt++
			if attempt >= i.policy.MaxAttempts {
				return err
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
				return ctx.Err()
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
