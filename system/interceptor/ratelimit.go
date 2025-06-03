package interceptor

import (
	"context"
	"fmt"
	"sync"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
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
func (i *RateLimitInterceptor) Handle(ctx context.Context, next func() error, _ ...apiinterceptor.Option) error {
	fmt.Println("RateLimitInterceptor")

	i.mu.Lock()
	if i.limiter == nil {
		i.limiter = rate.NewLimiter(rate.Limit(i.limit.RequestsPerSecond), i.limit.Burst)
	}
	i.mu.Unlock()

	if err := i.limiter.Wait(ctx); err != nil {
		return err
	}

	return next()
}

// Format implements the payload.Payload interface
func (i *RateLimitInterceptor) Format() payload.Format {
	return payload.Golang
}

// Data implements the payload.Payload interface
func (i *RateLimitInterceptor) Data() any {
	return i
}
