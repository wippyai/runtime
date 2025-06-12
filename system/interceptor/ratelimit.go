package interceptor

import (
	"context"
	"fmt"
	"sync"

	"github.com/hashicorp/golang-lru/v2/expirable"
	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/security"
	"golang.org/x/time/rate"
)

// RateLimitInterceptor implements rate limiting functionality
type RateLimitInterceptor struct {
	cache *expirable.LRU[string, *rate.Limiter]
	mu    sync.Mutex // Protects concurrent creation of limiters
}

// NewRateLimitInterceptor creates a new rate limit interceptor with the given limits
func NewRateLimitInterceptor(cache *expirable.LRU[string, *rate.Limiter]) *RateLimitInterceptor {
	return &RateLimitInterceptor{
		cache: cache,
	}
}

// Handle implements the interceptor interface
func (i *RateLimitInterceptor) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	options := apiinterceptor.GetOptionsFromContext(ctx)

	// If requests per second is 0, skip rate limiting
	if options.RateLimit.RequestsPerSecond == 0 {
		return next(ctx)
	}

	pid, ok := pubsub.GetPID(ctx)
	if !ok {
		// Handle case where PID is not found in context
		return &runtime.Result{Error: fmt.Errorf("PID not found in context")}, ctx
	}

	// Get actor ID from context, use empty string if not present
	actor, hasActor := security.GetActor(ctx)
	actorID := ""
	if hasActor {
		actorID = actor.ID
	}

	// Create cache key using actor ID and function name
	cacheKey := fmt.Sprintf("%s:%s", actorID, pid.ID)

	// Get or create rate limiter for this PID
	limiter, ok := i.cache.Get(cacheKey)
	if !ok {
		// Use configured values or fallback to defaults
		rps := options.RateLimit.RequestsPerSecond
		if rps != 0 {
			burst := options.RateLimit.Burst

			// Synchronize limiter creation to prevent race conditions
			i.mu.Lock()
			// Double-check after acquiring lock
			limiter, ok = i.cache.Get(cacheKey)
			if !ok {
				limiter = rate.NewLimiter(rate.Limit(rps), burst)
				i.cache.Add(cacheKey, limiter)
			}
			i.mu.Unlock()
		}
	}

	// Wait for rate limit with context cancellation
	err := limiter.Wait(ctx)
	if err != nil {
		return &runtime.Result{Error: err}, ctx
	}

	return next(ctx)
}

// Format implements the payload.Payload interface
func (i *RateLimitInterceptor) Format() payload.Format {
	return payload.Golang
}

// Data implements the payload.Payload interface
func (i *RateLimitInterceptor) Data() any {
	return i
}
