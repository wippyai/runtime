package interceptor

import (
	"context"

	"github.com/wippyai/runtime/api/function"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
	"github.com/wippyai/runtime/api/runtime"
)

// Chain represents a sequence of interceptors that can be executed in order
type Chain struct {
	interceptors []apiinterceptor.Interceptor
}

// newChain creates a new Chain with the given interceptors
func newChain(interceptors []apiinterceptor.Interceptor) Chain {
	return Chain{
		interceptors: interceptors,
	}
}

// Execute executes the chain of interceptors
func (c Chain) Execute(ctx context.Context, f function.Func, task runtime.Task) (chan *runtime.Result, error) {
	resultChan := make(chan *runtime.Result, 1)

	next := c.getNext(ctx, resultChan, 0, f, task)
	result, _ := next(ctx)
	if result != nil && result.Error != nil {
		close(resultChan)
		return nil, result.Error
	}

	resultChan <- result

	return resultChan, nil
}

func (c Chain) getNext(_ context.Context, resultChan chan *runtime.Result, index int, f function.Func, task runtime.Task) func(context.Context) (*runtime.Result, context.Context) {
	if index >= len(c.interceptors) {
		return func(ctx context.Context) (*runtime.Result, context.Context) {
			ch, err := f(ctx, task)
			if err != nil {
				return &runtime.Result{Error: err}, ctx
			}

			result := <-ch
			if result != nil && result.Error != nil {
				return result, ctx
			}

			return result, ctx
		}
	}

	interceptor := c.interceptors[index]

	return func(ctx context.Context) (*runtime.Result, context.Context) {
		nextFn := c.getNext(ctx, resultChan, index+1, f, task)
		result, newCtx := interceptor.Handle(ctx, nextFn)
		return result, newCtx
	}
}
