// Package interceptor provides request and operation interception.
package interceptor

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/runtime"
)

const (
	System event.System = "interceptor"

	Register event.Kind = "interceptor.register"
	Delete   event.Kind = "interceptor.delete"

	Accept event.Kind = "interceptor.accept"
	Reject event.Kind = "interceptor.reject"
)

type (
	// Interceptor defines the interface for function execution interceptors.
	// Interceptors can inspect/modify the task and control execution flow synchronously.
	Interceptor interface {
		Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error)
	}

	// HandlerFunc is a function adapter for the Interceptor interface.
	HandlerFunc func(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error)

	// Entry holds the interceptor and its order for registration.
	Entry struct {
		Interceptor Interceptor
		Order       int
	}

	// Chain represents a sequence of interceptors that can be executed in order synchronously.
	Chain interface {
		Execute(ctx context.Context, f function.Func, task runtime.Task) (*runtime.Result, error)
	}

	// Registry manages interceptor registration and execution.
	Registry interface {
		Chain
		Register(name string, interceptor Interceptor, order int) error
		Unregister(name string) error
	}
)

// Handle implements the Interceptor interface.
func (f HandlerFunc) Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	return f(ctx, task, next)
}
