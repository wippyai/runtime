package interceptor

import (
	"context"
)

// Option defines a function that configures an interceptor
type Option func(*Config)

// Config holds the configuration for an interceptor
type Config struct {
}

// Interceptor defines the interface for function execution interceptors
type Interceptor interface {
	// Handle processes the execution and calls next() to continue the chain
	Handle(ctx context.Context, next func() error, opts ...Option) error
}

// Registry interface provides access to the interceptor chain
type Registry interface {
	GetChain() Chain
}

// Chain represents a sequence of interceptors that can be executed in order
type Chain interface {
	// Execute executes the chain of interceptors
	Execute(ctx context.Context) error
}
