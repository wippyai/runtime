package interceptor

import (
	"context"
	"fmt"
)

// Chain represents a chain of interceptors
type Chain struct {
	interceptors []Interceptor
}

// NewChain creates a new interceptor chain
func NewChain(interceptors ...Interceptor) *Chain {
	return &Chain{
		interceptors: interceptors,
	}
}

// Execute executes the function with the interceptor chain
func (c *Chain) Execute(ctx context.Context, execution *Execution, fn func(context.Context, *Execution) (interface{}, error)) (interface{}, error) {
	// Execute before hooks
	for _, interceptor := range c.interceptors {
		if err := interceptor.Before(ctx, execution); err != nil {
			return nil, fmt.Errorf("interceptor before hook failed: %w", err)
		}
	}

	// Execute the function
	result, err := fn(ctx, execution)
	execution.Result = result
	execution.Error = err

	// Execute after hooks in reverse order
	for i := len(c.interceptors) - 1; i >= 0; i-- {
		interceptor := c.interceptors[i]
		if err := interceptor.After(ctx, execution, result, err); err != nil {
			return nil, fmt.Errorf("interceptor after hook failed: %w", err)
		}
	}

	return result, err
}

// AddInterceptor adds an interceptor to the chain
func (c *Chain) AddInterceptor(interceptor Interceptor) {
	c.interceptors = append(c.interceptors, interceptor)
}

// GetInterceptors returns all interceptors in the chain
func (c *Chain) GetInterceptors() []Interceptor {
	return c.interceptors
}
