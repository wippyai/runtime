package queue_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
)

func TestInterceptorInterfaces(t *testing.T) {
	t.Run("PublishInterceptor interface", func(t *testing.T) {
		ctx := context.Background()
		queueID := registry.ID{NS: "test", Name: "my-queue"}
		var msgs []*queue.Message
		msgs = append(msgs, queue.NewMessage(payload.New("test")))

		// Create a simple interceptor
		called := false
		interceptor := &simpleInterceptor{
			handleFunc: func(ctx context.Context, q registry.ID, msgs []*queue.Message,
				next func(context.Context, registry.ID, []*queue.Message) error) error {
				called = true
				return next(ctx, q, msgs)
			},
		}

		// Test the interceptor
		err := interceptor.Handle(ctx, queueID, msgs, func(_ context.Context, _ registry.ID, _ []*queue.Message) error {
			return nil
		})

		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("PublishInterceptorRegistry interface", func(t *testing.T) {
		// Note: The actual implementation will be in the system layer
		// This just verifies the interface is defined correctly
		var reg queue.PublishInterceptorRegistry
		assert.Nil(t, reg) // Implementation will be provided by system layer
	})

	t.Run("PublishChain interface", func(t *testing.T) {
		// Note: The actual implementation will be in the system layer
		// This just verifies the interface is defined correctly
		var chain queue.PublishChain
		assert.Nil(t, chain) // Implementation will be provided by system layer
	})
}

// simpleInterceptor is a test implementation of PublishInterceptor
type simpleInterceptor struct {
	handleFunc func(context.Context, registry.ID, []*queue.Message, func(context.Context, registry.ID, []*queue.Message) error) error
}

func (i *simpleInterceptor) Handle(ctx context.Context, q registry.ID, msgs []*queue.Message,
	next func(context.Context, registry.ID, []*queue.Message) error) error {
	return i.handleFunc(ctx, q, msgs, next)
}
