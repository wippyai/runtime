package interceptor

import (
	"context"
	"time"

	"github.com/ponyruntime/pony/api/runtime"
)

// Execution represents a function execution context
type Execution struct {
	FunctionID   string
	Options      map[string]interface{}
	Context      context.Context
	Interceptors []Interceptor
	Result       interface{}
	Error        error
	StartTime    time.Time
	EndTime      time.Time
	// Exported index to track current interceptor position
	CurrentIndex int
}

// Interceptor defines the interface for function execution interceptors
type Interceptor interface {
	// Handle processes the execution and calls next() to continue the chain
	Handle(ctx context.Context, task *runtime.Task, next func() error, opts ...Option) error
}
