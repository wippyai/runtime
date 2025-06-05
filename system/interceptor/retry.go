package interceptor

import (
	"context"
	"fmt"

	"github.com/davecgh/go-spew/spew"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
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

// extractRetryConfig extracts retry configuration from the context and updates the config
func extractRetryConfig(ctx context.Context, config *apiinterceptor.Config, defaultPolicy *apiinterceptor.RetryPolicy) (*apiinterceptor.RetryPolicy, error) {
	// Get registry from context
	registry := registry.GetRegistry(ctx)
	if registry == nil {
		return nil, fmt.Errorf("registry not found in context")
	}

	// Get PID from context
	pid, ok := pubsub.GetPID(ctx)
	if !ok {
		return nil, fmt.Errorf("PID not found in context")
	}

	// Get the function entry using PID
	entry, err := registry.GetEntry(pid.ID)
	if err != nil {
		return nil, err
	}

	payload := entry.Data.Data()
	spew.Dump(payload)

	// Extract retry configuration from pool configuration
	poolConfig, ok := payload.(map[string]interface{})["pool"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid pool configuration")
	}

	// Create a new policy based on the default
	policy := &apiinterceptor.RetryPolicy{
		MaxAttempts: defaultPolicy.MaxAttempts,
	}

	// Get retry attempts from pool config if it exists
	if retryAttempts, exists := poolConfig["retry_attempts"].(float64); exists {
		policy.MaxAttempts = int(retryAttempts)
	}

	spew.Dump("retry policy", policy)
	return policy, nil
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

	// Extract retry configuration
	policy, err := extractRetryConfig(ctx, config, i.policy)
	if err != nil {
		return &runtime.Result{Error: err}
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
			if attempt >= policy.MaxAttempts {
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
