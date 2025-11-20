package interceptor

import (
	"context"

	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

// Chain represents a sequence of interceptors that can be executed in order
type Chain struct {
	interceptors []function.Interceptor
	logger       *zap.Logger
}

// newChain creates a new Chain with the given interceptors
func newChain(interceptors []function.Interceptor, logger *zap.Logger) Chain {
	return Chain{
		interceptors: interceptors,
		logger:       logger,
	}
}

// Execute executes the chain of interceptors synchronously
func (c Chain) Execute(ctx context.Context, f function.Func, task runtime.Task) (*runtime.Result, error) {
	if len(c.interceptors) == 0 {
		return f(ctx, task)
	}

	next := c.buildNext(0, f)
	return next(ctx, task)
}

func (c Chain) buildNext(index int, f function.Func) func(context.Context, runtime.Task) (*runtime.Result, error) {
	if index >= len(c.interceptors) {
		// Final step - call actual function
		return f
	}

	interceptor := c.interceptors[index]
	nextFunc := c.buildNext(index+1, f)

	return func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		return interceptor.Handle(ctx, task, nextFunc)
	}
}
