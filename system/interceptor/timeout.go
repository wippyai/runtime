package interceptor

import (
	"context"
	"fmt"
	"time"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/runtime"
)

// TimeoutInterceptor implements timeout functionality
type TimeoutInterceptor struct{}

// NewTimeoutInterceptor creates a new timeout interceptor with the given timeout duration
func NewTimeoutInterceptor() *TimeoutInterceptor {
	return &TimeoutInterceptor{}
}

// Handle implements the interceptor interface
func (i *TimeoutInterceptor) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	// Create config and apply options
	options := apiinterceptor.GetOptionsFromContext(ctx)

	// Use configured timeout or fallback to default
	timeout := options.Timeout.Timeout
	// If timeout is 0, skip timeout
	if timeout == 0 {
		return next(ctx)
	}

	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout))
	defer cancel()

	// Create a channel to receive the result
	resultChan := make(chan *runtime.Result, 1)
	contextChan := make(chan context.Context, 1)

	// Execute the next interceptor in a goroutine
	go func() {
		result, newCtx := next(timeoutCtx)
		resultChan <- result
		contextChan <- newCtx
	}()

	// Wait for either the timeout or the result
	select {
	case <-timeoutCtx.Done():
		if timeoutCtx.Err() == context.DeadlineExceeded {
			// Check if the original context is also done
			select {
			case <-ctx.Done():
				return &runtime.Result{Error: ctx.Err()}, ctx
			default:
				cancelFunc := apiinterceptor.GetCancelFromContext(ctx)
				cancelFunc()

				return &runtime.Result{Error: fmt.Errorf("operation timed out after %dms", time.Duration(timeout)/time.Millisecond)}, ctx
			}
		}

		return &runtime.Result{Error: timeoutCtx.Err()}, ctx
	case result := <-resultChan:
		newCtx := <-contextChan
		return result, newCtx
	}
}
