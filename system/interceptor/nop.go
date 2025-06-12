package interceptor

import (
	"context"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
)

// NopInterceptor is a no-operation interceptor that does nothing
type NopInterceptor struct{}

// NewNopInterceptor creates a new no-operation interceptor
func NewNopInterceptor() *NopInterceptor {
	return &NopInterceptor{}
}

// Handle implements the interceptor interface
func (i *NopInterceptor) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	return next(ctx)
}

// Format implements the payload.Payload interface
func (i *NopInterceptor) Format() payload.Format {
	return payload.Golang
}

// Data implements the payload.Payload interface
func (i *NopInterceptor) Data() any {
	return i
}
