package interceptor

import (
	"context"

	"github.com/ponyruntime/pony/api/runtime"
)

// Interceptor defines the interface for function execution interceptors
type Interceptor interface {
	// Handle processes the execution and calls next() to continue the chain
	Handle(ctx context.Context, task *runtime.Task, next func() error) error
}

// Registry interface provides access to the interceptor chain
type Registry interface {
	GetChain() Chain
}

// Chain represents a sequence of interceptors that can be executed in order
type Chain interface {
	// Execute executes the chain of interceptors
	Execute(ctx context.Context, task runtime.Task) error
}
