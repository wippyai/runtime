package function

import (
	"context"

	"github.com/wippyai/runtime/api/runtime"
)

type (
	// Interceptor defines the interface for function execution interceptors.
	// Interceptors can inspect/modify the task and control execution flow synchronously.
	Interceptor interface {
		Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error)
	}

	// InterceptorChain represents a sequence of interceptors that can be executed in order synchronously.
	InterceptorChain interface {
		Execute(ctx context.Context, f Func, task runtime.Task) (*runtime.Result, error)
	}

	// InterceptorRegistry manages interceptor registration and execution.
	InterceptorRegistry interface {
		InterceptorChain
		Register(name string, interceptor Interceptor, order int) error
		Unregister(name string) error
	}
)
