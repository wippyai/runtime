package interceptor

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	"golang.org/x/time/rate"
)

// RateLimitInterceptor implements rate limiting functionality
type RateLimitInterceptor struct {
	limit   apiinterceptor.RateLimit
	limiter *rate.Limiter
	mu      sync.Mutex
}

// NewRateLimitInterceptor creates a new rate limit interceptor with the given limit
func NewRateLimitInterceptor(limit apiinterceptor.RateLimit) *RateLimitInterceptor {
	return &RateLimitInterceptor{
		limit: limit,
	}
}

// Handle implements the interceptor interface
func (i *RateLimitInterceptor) Handle(ctx context.Context, next func() *runtime.Result, opts ...apiinterceptor.Option) *runtime.Result {
	fmt.Println("RateLimitInterceptor")

	// Create config and apply options
	config := &apiinterceptor.Config{}
	for _, opt := range opts {
		opt(config)
	}

	i.mu.Lock()
	if i.limiter == nil {
		i.limiter = rate.NewLimiter(rate.Limit(i.limit.RequestsPerSecond), i.limit.Burst)
	}
	i.mu.Unlock()

	if err := i.limiter.Wait(ctx); err != nil {
		return &runtime.Result{Error: err}
	}

	result := next()

	// If we have a response, check for rate limit headers
	if config.Response != nil {
		// Check for Retry-After header
		if retryAfter := config.Response.Header.Get("Retry-After"); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil {
				// Update the limiter's rate based on the server's response
				i.mu.Lock()
				i.limiter.SetLimit(rate.Limit(1.0 / float64(seconds)))
				i.mu.Unlock()
			}
		}

		// Check for X-RateLimit-* headers
		if remaining := config.Response.Header.Get("X-RateLimit-Remaining"); remaining != "" {
			if rem, err := strconv.Atoi(remaining); err == nil && rem > 0 {
				// Update the limiter's burst based on remaining requests
				i.mu.Lock()
				i.limiter.SetBurst(rem)
				i.mu.Unlock()
			}
		}
	}

	return result
}

// Format implements the payload.Payload interface
func (i *RateLimitInterceptor) Format() payload.Format {
	return payload.Golang
}

// Data implements the payload.Payload interface
func (i *RateLimitInterceptor) Data() any {
	return i
}
