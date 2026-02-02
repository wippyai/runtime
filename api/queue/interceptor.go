package queue

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
)

// PublishNext is the function type for calling the next interceptor in chain
type PublishNext = func(context.Context, registry.ID, []*Message) error

// PublishInterceptor intercepts message publishing for cross-cutting concerns
type PublishInterceptor interface {
	Handle(ctx context.Context, queue registry.ID, msgs []*Message, next PublishNext) error
}

// PublishChain executes a chain of interceptors
type PublishChain interface {
	// Publish executes the interceptor chain for publishing messages
	Publish(ctx context.Context, queue registry.ID, msgs ...*Message) error
}

// PublishInterceptorRegistry manages publish interceptors
type PublishInterceptorRegistry interface {
	PublishChain
	// Register adds an interceptor with the given name and priority
	// Lower priority values execute first
	Register(name string, interceptor PublishInterceptor, priority int)

	// Unregister removes an interceptor by name
	Unregister(name string)
}
