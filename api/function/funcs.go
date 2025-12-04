// Package function provides abstractions for managing and executing functions.
package function

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/runtime"
)

// System identifies the function system in the event bus.
const System event.System = "function"

// Event kinds for function operations.
const (
	Register event.Kind = "function.register"
	Delete   event.Kind = "function.delete"
	Accept   event.Kind = "function.accept"
	Reject   event.Kind = "function.reject"
)

type (
	// Func processes tasks synchronously and returns the result.
	Func func(context.Context, runtime.Task) (*runtime.Result, error)

	// FuncEntry holds both the function handler and its options for registration.
	FuncEntry struct {
		Handler Func
		Options runtime.Options
	}

	// Registry defines the interface for managing and executing functions.
	Registry interface {
		// Call executes a function identified by the task synchronously.
		Call(context.Context, runtime.Task) (*runtime.Result, error)
	}

	// Interceptor defines the interface for function execution interceptors.
	Interceptor interface {
		Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error)
	}

	// InterceptorRegistry manages interceptor registration and execution.
	InterceptorRegistry interface {
		Execute(ctx context.Context, f Func, task runtime.Task) (*runtime.Result, error)
		Register(name string, interceptor Interceptor, order int) error
		Unregister(name string) error
	}
)
