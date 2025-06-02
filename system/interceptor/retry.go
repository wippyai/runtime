package interceptor

import (
	"context"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
)

// RetryInterceptor implements retry functionality
type RetryInterceptor struct {
	policy *interceptor.RetryPolicy
}

// NewRetryInterceptor creates a new retry interceptor with the given policy
func NewRetryInterceptor(policy *interceptor.RetryPolicy) *RetryInterceptor {
	return &RetryInterceptor{
		policy: policy,
	}
}

// Handle implements the interceptor interface
func (i *RetryInterceptor) Handle(ctx context.Context, _ *runtime.Task, next func() error) error {
	var err error
	attempt := 0

	fmt.Println("RetryInterceptor")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			err = next()
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
