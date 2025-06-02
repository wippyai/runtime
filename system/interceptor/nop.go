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
func (i *NopInterceptor) Handle(_ context.Context, _ *runtime.Task, next func() error) error {
	return next()
}

// Format implements the payload.Payload interface
func (i *NopInterceptor) Format() payload.Format {
	return payload.Golang
}

// Data implements the payload.Payload interface
func (i *NopInterceptor) Data() any {
	return i
}
