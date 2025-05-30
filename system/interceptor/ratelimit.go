package interceptor

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	"golang.org/x/time/rate"
)

// RateLimitInterceptor implements rate limiting functionality
type RateLimitInterceptor struct {
	limit   interceptor.RateLimit
	limiter *rate.Limiter
	mu      sync.Mutex
}

// NewRateLimitInterceptor creates a new rate limit interceptor with the given limit
func NewRateLimitInterceptor(limit interceptor.RateLimit) *RateLimitInterceptor {
	return &RateLimitInterceptor{
		limit: limit,
	}
}

// Handle implements the interceptor interface
func (i *RateLimitInterceptor) Handle(ctx context.Context, task *runtime.Task, next func() error, opts ...Option) error {

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
