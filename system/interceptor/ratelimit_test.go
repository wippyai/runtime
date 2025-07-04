package interceptor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestRateLimitInterceptor(t *testing.T) {
	// Create a cache with a short TTL for testing
	cache := expirable.NewLRU[string, *rate.Limiter](100, nil, 100*time.Millisecond)
	interceptor := NewRateLimitInterceptor(cache)

	tests := []struct {
		name           string
		rps            int
		burst          int
		pid            pubsub.PID
		contextTimeout time.Duration
		nextFunc       func() *runtime.Result
		expectedError  error
		description    string
	}{
		{
			name:  "skip when rps is 0",
			rps:   0,
			burst: 1,
			pid:   pubsub.PID{},
			nextFunc: func() *runtime.Result {
				return &runtime.Result{}
			},
			expectedError: nil,
			description:   "should skip rate limiting when RPS is 0",
		},
		{
			name:  "error when pid not in context",
			rps:   1,
			burst: 1,
			pid:   pubsub.PID{},
			nextFunc: func() *runtime.Result {
				return &runtime.Result{}
			},
			expectedError: assert.AnError,
			description:   "should return error when PID is not in context",
		},
		{
			name:  "successful rate limiting",
			rps:   10,
			burst: 5,
			pid:   pubsub.PID{ID: registry.ID{Name: "test-pid"}},
			nextFunc: func() *runtime.Result {
				return &runtime.Result{}
			},
			expectedError: nil,
			description:   "should successfully rate limit requests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.contextTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.contextTimeout)
				defer cancel()
			}

			// Add PID to context if provided
			if tt.pid.ID.Name != "" {
				ctx = pubsub.WithPID(ctx, tt.pid)
			}

			// Create options with rate limit settings
			options := apiinterceptor.Options{
				RateLimit: apiinterceptor.RateLimitOptions{
					RequestsPerSecond: tt.rps,
					Burst:             tt.burst,
				},
			}
			ctx = apiinterceptor.WithOptions(ctx, options)

			// Execute the interceptor
			result, _ := interceptor.Handle(ctx, func(ctx context.Context) (*runtime.Result, context.Context) {
				return tt.nextFunc(), ctx
			})

			if tt.expectedError != nil {
				assert.Error(t, result.Error)
				if errors.Is(tt.expectedError, assert.AnError) {
					assert.Equal(t, "PID not found in context", result.Error.Error())
				} else {
					assert.Equal(t, tt.expectedError, result.Error)
				}
			} else {
				assert.NoError(t, result.Error)
			}
		})
	}
}

func TestRateLimitInterceptorConcurrent(t *testing.T) {
	// Create a cache with a short TTL for testing
	cache := expirable.NewLRU[string, *rate.Limiter](100, nil, 100*time.Millisecond)
	interceptor := NewRateLimitInterceptor(cache)

	// Test concurrent requests with rate limiting
	pid := pubsub.PID{ID: registry.ID{Name: "test-pid"}}
	options := apiinterceptor.Options{
		RateLimit: apiinterceptor.RateLimitOptions{
			RequestsPerSecond: 10,
			Burst:             5,
		},
	}

	// Create a channel to coordinate goroutines
	start := make(chan struct{})
	done := make(chan struct{})

	// Launch multiple concurrent requests
	const numRequests = 10
	results := make([]*runtime.Result, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			<-start // Wait for start signal
			ctx := pubsub.WithPID(context.Background(), pid)
			ctx = apiinterceptor.WithOptions(ctx, options)
			results[idx], _ = interceptor.Handle(ctx, func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{}, ctx
			})
			done <- struct{}{}
		}(i)
	}

	// Start all requests
	close(start)

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		<-done
	}

	// Verify results
	for i, result := range results {
		assert.NoError(t, result.Error, "request %d should succeed", i)
	}
}

func TestRateLimitInterceptorTiming(t *testing.T) {
	// Create a cache with a short TTL for testing
	cache := expirable.NewLRU[string, *rate.Limiter](100, nil, 5*time.Second)
	interceptor := NewRateLimitInterceptor(cache)

	// Test concurrent requests with rate limiting
	pid := pubsub.PID{ID: registry.ID{Name: "test-pid"}}
	options := apiinterceptor.Options{
		RateLimit: apiinterceptor.RateLimitOptions{
			RequestsPerSecond: 10, // 10 requests per second
			Burst:             5,  // Allow 5 requests to burst
		},
	}

	// Create a channel to coordinate goroutines
	start := make(chan struct{})
	done := make(chan struct{})

	const numRequests = 20 // More requests than burst to ensure rate limiting kicks in
	results := make([]*runtime.Result, numRequests)
	testStart := time.Now()

	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			<-start // Wait for start signal
			ctx := pubsub.WithPID(context.Background(), pid)
			ctx = apiinterceptor.WithOptions(ctx, options)
			results[idx], _ = interceptor.Handle(ctx, func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{}, ctx
			})
			done <- struct{}{}
		}(i)
	}

	// Start all requests
	close(start)

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		<-done
	}

	// Verify results
	successCount := 0
	for _, result := range results {
		if result.Error == nil {
			successCount++
		} else {
			assert.Contains(t, result.Error.Error(), "rate limit exceeded", "error should indicate rate limit exceeded")
		}
	}

	// All requests should succeed since we're using Wait() which blocks until rate limit allows
	assert.Equal(t, numRequests, successCount, "all requests should succeed")

	// Verify total test duration
	totalDuration := time.Since(testStart)
	expectedMinDuration := time.Duration(numRequests-5) * 100 * time.Millisecond // 100ms per request after burst
	assert.Greater(t, totalDuration, expectedMinDuration, "total duration should reflect rate limiting")
}
