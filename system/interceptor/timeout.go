package interceptor

import (
	"context"
	"fmt"
	"time"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
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

// Handle implements the interceptor interface
func (i *TimeoutInterceptor) Handle(ctx context.Context, next func() *runtime.Result, opts ...apiinterceptor.Option) *runtime.Result {
	// Create config and apply options
	config := &apiinterceptor.Config{}
	for _, opt := range opts {
		opt(config)
	}

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, i.timeout)
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
			return &runtime.Result{Error: fmt.Errorf("operation timed out after %v", i.timeout)}
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
