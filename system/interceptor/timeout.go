package interceptor

import (
	"context"
	"fmt"
	"time"

	"github.com/davecgh/go-spew/spew"
	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
)

// TimeoutInterceptor implements timeout functionality
type TimeoutInterceptor struct {
	timeout time.Duration
}

// NewTimeoutInterceptor creates a new timeout interceptor with the given timeout duration
func NewTimeoutInterceptor(timeout time.Duration) *TimeoutInterceptor {
	return &TimeoutInterceptor{
		timeout: timeout,
	}
}

// extractTimeoutConfig extracts timeout configuration from the context and updates the config
func extractTimeoutConfig(ctx context.Context, config *apiinterceptor.Config, defaultTimeout time.Duration) (time.Duration, error) {
	// Get registry from context
	registry := registry.GetRegistry(ctx)
	if registry == nil {
		return 0, fmt.Errorf("registry not found in context")
	}

	// Get PID from context
	pid, ok := pubsub.GetPID(ctx)
	if !ok {
		return 0, fmt.Errorf("PID not found in context")
	}

	// Get the function entry using PID
	entry, err := registry.GetEntry(pid.ID)
	if err != nil {
		return 0, err
	}

	payload := entry.Data.Data()
	spew.Dump(payload)

	// Extract timeout from pool configuration
	poolConfig, ok := payload.(map[string]interface{})["pool"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("invalid pool configuration")
	}

	// Get timeout from pool config or use default from policy
	var timeout time.Duration
	if timeoutStr, exists := poolConfig["timeout"].(string); exists {
		var err error
		timeout, err = time.ParseDuration(timeoutStr)
		if err != nil {
			return 0, fmt.Errorf("invalid timeout duration: %v", err)
		}
	} else {
		// Use default timeout from policy
		timeout = defaultTimeout
	}

	spew.Dump("timeout", timeout)
	return timeout, nil
}

// Handle implements the interceptor interface
func (i *TimeoutInterceptor) Handle(ctx context.Context, next func() *runtime.Result, opts ...apiinterceptor.Option) *runtime.Result {
	// Create config and apply options
	config := &apiinterceptor.Config{}
	for _, opt := range opts {
		opt(config)
	}

	timeout, err := extractTimeoutConfig(ctx, config, i.timeout)
	if err != nil {
		return &runtime.Result{Error: err}
	}

	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create a channel to receive the result
	resultChan := make(chan *runtime.Result, 1)

	// Execute the next interceptor in a goroutine
	go func() {
		resultChan <- next()
	}()

	// Wait for either the timeout or the result
	select {
	case <-timeoutCtx.Done():
		if timeoutCtx.Err() == context.DeadlineExceeded {
			if config.CancelFunc != nil {
				config.CancelFunc()
			}
			return &runtime.Result{Error: fmt.Errorf("operation timed out after %v", timeout)}
		}
		return &runtime.Result{Error: timeoutCtx.Err()}
	case result := <-resultChan:
		return result
	}
}

// Format implements the payload.Payload interface
func (i *TimeoutInterceptor) Format() payload.Format {
	return payload.Golang
}

// Data implements the payload.Payload interface
func (i *TimeoutInterceptor) Data() any {
	return i
}
