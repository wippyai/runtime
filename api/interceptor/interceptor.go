// Package interceptor provides request and operation interception.
package interceptor

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/runtime"
)

// Event system and kind constants
const (
	System event.System = "interceptor"

	Register event.Kind = "interceptor.register"
	Update   event.Kind = "interceptor.update"
	Delete   event.Kind = "interceptor.delete"

	Accept event.Kind = "interceptor.accept"
	Reject event.Kind = "interceptor.reject"
)

// Interceptor defines the interface for function execution interceptors
type Interceptor interface {
	Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context)
}

// InterceptorFunc is a function adapter for the Interceptor interface
type InterceptorFunc func(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context)

// Handle implements the Interceptor interface
func (f InterceptorFunc) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	return f(ctx, next)
}

// Chain represents a sequence of interceptors that can be executed in order
type Chain interface {
	Execute(ctx context.Context, f function.Func, task runtime.Task) (chan *runtime.Result, error)
}

// Options defines a flexible bag interface for interceptor configuration
type Options interface {
	Get(key string) (any, bool)
	GetString(key string, def string) string
	GetInt(key string, def int) int
	GetBool(key string, def bool) bool
	GetDuration(key string, def time.Duration) time.Duration
	Merge(other Options) Options
	Clone() Options
}
