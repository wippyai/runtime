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

func TestRetryInterceptor(t *testing.T) {
	tests := []struct {
		name           string
		maxAttempts    int
		nextFunc       func(context.Context) (*runtime.Result, context.Context)
		expectedError  error
		expectedCalls  int
		contextTimeout time.Duration
	}{
		{
			name:        "success on first attempt",
			maxAttempts: 3,
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{}, ctx
			},
			expectedError: nil,
			expectedCalls: 1,
		},
		{
			name:        "success after retries",
			maxAttempts: 3,
			nextFunc: func() func(context.Context) (*runtime.Result, context.Context) {
				calls := 0
				return func(ctx context.Context) (*runtime.Result, context.Context) {
					calls++
					if calls < 2 {
						return &runtime.Result{Error: errors.New("temporary error")}, ctx
					}
					return &runtime.Result{}, ctx
				}
			}(),
			expectedError: nil,
			expectedCalls: 2,
		},
		{
			name:        "max attempts reached",
			maxAttempts: 2,
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{Error: errors.New("persistent error")}, ctx
			},
			expectedError: errors.New("persistent error"),
			expectedCalls: 2,
		},
		{
			name:        "skip retry when max attempts is 0",
			maxAttempts: 0,
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{Error: errors.New("error")}, ctx
			},
			expectedError: errors.New("error"),
			expectedCalls: 1,
		},
		{
			name:           "context cancellation",
			maxAttempts:    3,
			contextTimeout: 100 * time.Millisecond,
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				time.Sleep(200 * time.Millisecond)
				return &runtime.Result{Error: errors.New("error")}, ctx
			},
			expectedError: context.DeadlineExceeded,
			expectedCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interceptor := NewRetryInterceptor()

			ctx := context.Background()
			if tt.contextTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.contextTimeout)
				defer cancel()
			}

			// Create options with max attempts
			options := apiinterceptor.Options{
				Retry: apiinterceptor.RetryOptions{
					MaxAttempts: tt.maxAttempts,
				},
			}
			ctx = apiinterceptor.WithOptions(ctx, options)

			// Track number of calls
			calls := 0
			next := func(ctx context.Context) (*runtime.Result, context.Context) {
				calls++
				return tt.nextFunc(ctx)
			}

			result, _ := interceptor.Handle(ctx, next)

			// Verify results
			if tt.expectedError != nil {
				assert.Error(t, result.Error)
				assert.Equal(t, tt.expectedError.Error(), result.Error.Error())
			} else {
				assert.NoError(t, result.Error)
			}
			assert.Equal(t, tt.expectedCalls, calls)
		})
	}
}
