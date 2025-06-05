package interceptor

import (
	"context"
	"net/http"

	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/runtime"
)

// Option defines a function that configures an interceptor
type Option func(*Config)

// Config holds the configuration for an interceptor
type Config struct {
	Response   *http.Response
	CancelFunc context.CancelFunc
}

// WithResponse sets the HTTP response for the interceptor
func WithResponse(resp *http.Response) Option {
	return func(c *Config) {
		c.Response = resp
	}
}

// WithCancel sets the cancel function for the interceptor
func WithCancel(cancel context.CancelFunc) Option {
	return func(c *Config) {
		c.CancelFunc = cancel
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
