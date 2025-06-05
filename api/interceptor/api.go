package interceptor

import (
	"context"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
)

// Option defines a function that configures an interceptor
type Option func(*Config)

// Config holds the configuration for an interceptor
type Config struct {
	CancelFunc        context.CancelFunc
	Timeout           time.Duration
	MaxRetryAttempts  int
	RequestsPerSecond int
	Burst             int
}

// WithCancel sets the cancel function for the interceptor
func WithCancel(cancel context.CancelFunc) Option {
	return func(c *Config) {
		c.CancelFunc = cancel
	}
}

// WithTimeout sets the timeout for the interceptor
func WithTimeout(ctx context.Context, timeout time.Duration) Option {
	return func(c *Config) {
		// Get registry from context
		registry := registry.GetRegistry(ctx)
		if registry == nil {
			c.Timeout = timeout // Use default timeout if registry not found
			return
		}

		// Get PID from context
		pid, ok := pubsub.GetPID(ctx)
		if !ok {
			c.Timeout = timeout // Use default timeout if PID not found
			return
		}

		// Get the function entry using PID
		entry, err := registry.GetEntry(pid.ID)
		if err != nil {
			c.Timeout = timeout // Use default timeout if entry not found
			return
		}

		payload := entry.Data.Data()

		// Extract timeout from pool configuration
		poolConfig, ok := payload.(map[string]interface{})["pool"].(map[string]interface{})
		if !ok {
			c.Timeout = timeout // Use default timeout if pool config invalid
			return
		}

		// Get timeout from pool config or use default
		if timeoutStr, exists := poolConfig["timeout"].(string); exists {
			if parsedTimeout, err := time.ParseDuration(timeoutStr); err == nil {
				c.Timeout = parsedTimeout
				return
			}
		}

		c.Timeout = timeout // Use default timeout if no valid timeout found
	}
}

// WithRetry sets the retry attempts for the interceptor
func WithRetry(ctx context.Context, defaultMaxAttempts int) Option {
	return func(c *Config) {
		// Get registry from context
		registry := registry.GetRegistry(ctx)
		if registry == nil {
			c.MaxRetryAttempts = defaultMaxAttempts // Use default if registry not found
			return
		}

		// Get PID from context
		pid, ok := pubsub.GetPID(ctx)
		if !ok {
			c.MaxRetryAttempts = defaultMaxAttempts // Use default if PID not found
			return
		}

		// Get the function entry using PID
		entry, err := registry.GetEntry(pid.ID)
		if err != nil {
			c.MaxRetryAttempts = defaultMaxAttempts // Use default if entry not found
			return
		}

		payload := entry.Data.Data()

		// Extract retry configuration from pool configuration
		poolConfig, ok := payload.(map[string]interface{})["pool"].(map[string]interface{})
		if !ok {
			c.MaxRetryAttempts = defaultMaxAttempts // Use default if pool config invalid
			return
		}

		// Get retry attempts from pool config if it exists
		if retryAttempts, exists := poolConfig["retry_attempts"].(float64); exists {
			c.MaxRetryAttempts = int(retryAttempts)
		} else {
			c.MaxRetryAttempts = defaultMaxAttempts
		}
	}
}

// WithRateLimit sets the rate limit for the interceptor
func WithRateLimit(ctx context.Context, defaultRequestsPerSecond, defaultBurst int) Option {
	return func(c *Config) {
		// Get registry from context
		registry := registry.GetRegistry(ctx)
		if registry == nil {
			c.RequestsPerSecond = defaultRequestsPerSecond
			c.Burst = defaultBurst
			return
		}

		// Get PID from context
		pid, ok := pubsub.GetPID(ctx)
		if !ok {
			c.RequestsPerSecond = defaultRequestsPerSecond
			c.Burst = defaultBurst
			return
		}

		// Get the function entry using PID
		entry, err := registry.GetEntry(pid.ID)
		if err != nil {
			c.RequestsPerSecond = defaultRequestsPerSecond
			c.Burst = defaultBurst
			return
		}

		payload := entry.Data.Data()

		// Extract rate limit configuration from pool configuration
		poolConfig, ok := payload.(map[string]interface{})["pool"].(map[string]interface{})
		if !ok {
			c.RequestsPerSecond = defaultRequestsPerSecond
			c.Burst = defaultBurst
			return
		}

		// Get rate limit from pool config if it exists
		if rps, exists := poolConfig["requests_per_second"].(float64); exists {
			c.RequestsPerSecond = int(rps)
		} else {
			c.RequestsPerSecond = defaultRequestsPerSecond
		}

		if burst, exists := poolConfig["burst"].(float64); exists {
			c.Burst = int(burst)
		} else {
			c.Burst = defaultBurst
		}

		spew.Dump("rate limit", c.RequestsPerSecond, c.Burst)
	}
}

// Interceptor defines the interface for function execution interceptors
type Interceptor interface {
	// Handle processes the execution and calls next() to continue the chain
	Handle(ctx context.Context, next func() *runtime.Result, opts ...Option) *runtime.Result
}

// Registry interface provides access to the interceptor chain
type Registry interface {
	GetChain() Chain
}

// Chain represents a sequence of interceptors that can be executed in order
type Chain interface {
	// Execute executes the chain of interceptors
	Execute(ctx context.Context, f function.Func, task runtime.Task, opts ...Option) (chan *runtime.Result, error)
}
