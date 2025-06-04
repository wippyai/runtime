package interceptor

import (
	"context"
	"fmt"

	"github.com/hashicorp/golang-lru/v2/expirable"
	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"golang.org/x/time/rate"
)

// RateLimitInterceptor implements rate limiting functionality
type RateLimitInterceptor struct {
	limit apiinterceptor.RateLimit
	cache *expirable.LRU[string, *rate.Limiter]
}

// NewRateLimitInterceptor creates a new rate limit interceptor with the given limit
func NewRateLimitInterceptor(limit apiinterceptor.RateLimit, cache *expirable.LRU[string, *rate.Limiter]) *RateLimitInterceptor {
	return &RateLimitInterceptor{
		limit: limit,
		cache: cache,
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

	pid, ok := pubsub.GetPID(ctx)
	if !ok {
		// Handle case where PID is not found in context
		return &runtime.Result{Error: fmt.Errorf("PID not found in context")}
	}

	pidStr := pid.String()

	// Get or create rate limiter for this PID
	limiter, ok := i.cache.Get(pidStr)
	if !ok {
		limiter = rate.NewLimiter(rate.Limit(i.limit.RequestsPerSecond), i.limit.Burst)
		i.cache.Add(pidStr, limiter)
	}

	// Wait for rate limit
	if err := limiter.Wait(ctx); err != nil {
		return &runtime.Result{Error: err}
	}

	result := next()

	fmt.Println("RateLimitInterceptor completed")

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
