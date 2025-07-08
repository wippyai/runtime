package interceptor

import (
	"context"
	"errors"
	"testing"
	"time"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/stretchr/testify/assert"
)

func TestTimeoutInterceptor(t *testing.T) {
	interceptor := NewTimeoutInterceptor()

	tests := []struct {
		name           string
		timeout        apiinterceptor.Duration
		nextFunc       func() *runtime.Result
		contextTimeout time.Duration
		expectedError  error
		description    string
	}{
		{
			name:    "skip when timeout is 0",
			timeout: 0,
			nextFunc: func() *runtime.Result {
				time.Sleep(100 * time.Millisecond)
				return &runtime.Result{}
			},
			expectedError: nil,
			description:   "should skip timeout when duration is 0",
		},
		{
			name:    "success within timeout",
			timeout: apiinterceptor.Duration(200 * time.Millisecond),
			nextFunc: func() *runtime.Result {
				time.Sleep(100 * time.Millisecond)
				return &runtime.Result{}
			},
			expectedError: nil,
			description:   "should complete successfully within timeout",
		},
		{
			name:    "timeout exceeded",
			timeout: apiinterceptor.Duration(100 * time.Millisecond),
			nextFunc: func() *runtime.Result {
				time.Sleep(200 * time.Millisecond)
				return &runtime.Result{}
			},
			expectedError: errors.New("operation timed out after 100ms"),
			description:   "should timeout when operation takes too long",
		},
		{
			name:    "error from next function",
			timeout: apiinterceptor.Duration(200 * time.Millisecond),
			nextFunc: func() *runtime.Result {
				time.Sleep(100 * time.Millisecond)
				return &runtime.Result{Error: errors.New("test error")}
			},
			expectedError: errors.New("test error"),
			description:   "should propagate error from next function",
		},
		{
			name:           "context cancellation",
			timeout:        apiinterceptor.Duration(200 * time.Millisecond),
			contextTimeout: 50 * time.Millisecond,
			nextFunc: func() *runtime.Result {
				time.Sleep(100 * time.Millisecond)
				return &runtime.Result{}
			},
			expectedError: context.DeadlineExceeded,
			description:   "should handle context cancellation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create base context
			ctx := context.Background()
			if tt.contextTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.contextTimeout)
				defer cancel()
			}

			// Add cancel function to context
			cancelFunc := func() {}
			ctx = apiinterceptor.WithCancel(ctx, cancelFunc)

			// Create options with timeout
			options := apiinterceptor.Options{
				Timeout: apiinterceptor.TimeoutOptions{
					Timeout: tt.timeout,
				},
			}
			ctx = apiinterceptor.WithOptions(ctx, options)

			// Execute the interceptor
			result, _ := interceptor.Handle(ctx, func(ctx context.Context) (*runtime.Result, context.Context) {
				return tt.nextFunc(), ctx
			})

			// Verify the result
			if tt.expectedError != nil {
				assert.Error(t, result.Error)
				assert.Equal(t, tt.expectedError.Error(), result.Error.Error())
			} else {
				assert.NoError(t, result.Error)
			}
		})
	}
}

func TestTimeoutInterceptorCancelPropagation(t *testing.T) {
	interceptor := NewTimeoutInterceptor()
	timeout := apiinterceptor.Duration(100 * time.Millisecond)

	// Track if cancel function was called
	cancelCalled := false
	cancelFunc := func() {
		cancelCalled = true
	}

	// Create context with options
	ctx := context.Background()
	ctx = apiinterceptor.WithCancel(ctx, cancelFunc)
	options := apiinterceptor.Options{
		Timeout: apiinterceptor.TimeoutOptions{
			Timeout: timeout,
		},
	}
	ctx = apiinterceptor.WithOptions(ctx, options)

	// Execute with delay exceeding timeout
	result, _ := interceptor.Handle(ctx, func(ctx context.Context) (*runtime.Result, context.Context) {
		time.Sleep(200 * time.Millisecond)
		return &runtime.Result{}, ctx
	})

	// Verify timeout occurred and cancel was called
	assert.Error(t, result.Error)
	assert.Equal(t, "operation timed out after 100ms", result.Error.Error())
	assert.True(t, cancelCalled, "cancel function should have been called")
}
