package interceptor

import (
	"context"

	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type Chain struct {
	interceptors []queueapi.PublishInterceptor
	publishFunc  func(context.Context, registry.ID, ...*queueapi.Message) error
	logger       *zap.Logger
}

func newChain(
	interceptors []queueapi.PublishInterceptor,
	publishFunc func(context.Context, registry.ID, ...*queueapi.Message) error,
	logger *zap.Logger,
) Chain {
	return Chain{
		interceptors: interceptors,
		publishFunc:  publishFunc,
		logger:       logger,
	}
}

func (c *Chain) Publish(ctx context.Context, queue registry.ID, msgs ...*queueapi.Message) error {
	if len(c.interceptors) == 0 {
		if c.publishFunc != nil {
			return c.publishFunc(ctx, queue, msgs...)
		}
		return queueapi.ErrNoPublishFunc
	}

	next := c.buildNext(0)
	return next(ctx, queue, msgs)
}

func (c *Chain) buildNext(index int) func(context.Context, registry.ID, []*queueapi.Message) error {
	if index >= len(c.interceptors) {
		return func(ctx context.Context, q registry.ID, msgs []*queueapi.Message) error {
			if c.publishFunc != nil {
				return c.publishFunc(ctx, q, msgs...)
			}
			return queueapi.ErrNoPublishFunc
		}
	}

	interceptor := c.interceptors[index]
	nextFunc := c.buildNext(index + 1)

	return func(ctx context.Context, q registry.ID, msgs []*queueapi.Message) error {
		return interceptor.Handle(ctx, q, msgs, nextFunc)
	}
}
