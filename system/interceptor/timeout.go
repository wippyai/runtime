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
type TimeoutInterceptor struct{}

// NewTimeoutInterceptor creates a new timeout interceptor with the given timeout duration
func NewTimeoutInterceptor() *TimeoutInterceptor {
	return &TimeoutInterceptor{}
}

// Handle implements the interceptor interface
func (i *TimeoutInterceptor) Handle(ctx context.Context, next func() *runtime.Result) *runtime.Result {
	// Create config and apply options

	// FIXME remove, added for debugging
	fmt.Println("TimeoutInterceptor")

	options := apiinterceptor.GetOptionsFromContext(ctx)

	// Use configured timeout or fallback to default
	timeout := options.Timeout.Timeout
	// If timeout is 0, skip timeout
	if timeout == 0 {
		fmt.Println("TimeoutInterceptor skipped")
		return next()
	}

	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout))
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
			cancelFunc := apiinterceptor.GetCancelFromContext(ctx)
			cancelFunc()

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
