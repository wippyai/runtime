// Package interceptor provides request and operation interception.
package interceptor

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
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
	Interceptor interface {
		Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context)
	}

	// HandlerFunc is a function adapter for the Interceptor interface.
	HandlerFunc func(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context)

	// Entry holds the interceptor and its order for registration.
	Entry struct {
		Interceptor Interceptor
		Order       int
	}

	// Chain represents a sequence of interceptors that can be executed in order.
	Chain interface {
		Execute(ctx context.Context, f function.Func, task runtime.Task) (chan *runtime.Result, error)
	}

	// Options is an alias to attrs.Attributes for interceptor configuration.
	Options = attrs.Attributes
)

// Handle implements the Interceptor interface.
func (f HandlerFunc) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	return f(ctx, next)
}
