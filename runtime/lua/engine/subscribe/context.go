package subscribe

import (
	"container/list"
	"context"
	"fmt"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// Define context key for subscribe layer
//

var subContext = ctxapi.Key{Name: "subscribe.context"}

// layerContext maintains state for subscribe operations
type layerContext struct {
	subs         *subscriptionContext // Subscription manager
	messageQueue *list.List           // Queue of pending messages, this layer does not drop unfamiliar messages
}

// getLayerContext retrieves the subscribe layer context from the UnitOfWork
func getLayerContext(uw engine.UnitOfWork) *layerContext {
	ctx, ok := uw.Values().Get(subContext)
	if !ok {
		return nil
	}

	if v, ok := ctx.(*layerContext); ok {
		return v
	}

	return nil
}

// ensureLayerContext gets or creates a subscribe layer context in the UnitOfWork
func ensureLayerContext(uw engine.UnitOfWork) *layerContext {
	if uw == nil {
		return nil
	}

	ctx, ok := uw.Values().Get(subContext)
	if !ok {
		ctx = &layerContext{
			subs:         newSubscriptionContext(),
			messageQueue: list.New(),
		}
		uw.Values().Set(subContext, ctx)
		return ctx.(*layerContext)
	}

	if v, ok := ctx.(*layerContext); ok {
		return v
	}

	return nil
}

// Publish adds a message to the queue for the specified topic
func Publish(ctx context.Context, topic string, values ...lua.LValue) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return fmt.Errorf("no unit of work found")
	}

	lCtx := ensureLayerContext(uw)
	if lCtx == nil {
		return fmt.Errorf("layer context not found in unit of work")
	}

	return uw.Tasks().Schedule(func() {
		lCtx.messageQueue.PushBack(&op{
			topic:  topic,
			values: values,
		})
	})
}

// Release removes a subscription from the topic
func Release(ctx context.Context, topic string) error {
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return fmt.Errorf("no unit of work found")
	}

	lCtx := ensureLayerContext(uw)
	if lCtx == nil {
		return fmt.Errorf("layer context not found in unit of work")
	}

	return uw.Tasks().Schedule(func() {
		lCtx.messageQueue.PushBack(&op{topic: topic, unsub: true})
	})
}

// Slots returns the number of available slots for the specified topic
func Slots(ctx context.Context, topic string) (int, error) {
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return 0, fmt.Errorf("no unit of work found")
	}

	lCtx := getLayerContext(uw)
	if lCtx == nil {
		return 0, fmt.Errorf("layer context not found in unit of work")
	}

	sub, exists := lCtx.subs.get(topic)
	if !exists {
		return 0, fmt.Errorf("no subscribers for topic %s", topic)
	}

	return sub.channel.Slots(), nil
}

// Exists checks if a topic has any subscribers
func Exists(ctx context.Context, topic string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return false, fmt.Errorf("no unit of work found")
	}

	lCtx := getLayerContext(uw)
	if lCtx == nil {
		return false, fmt.Errorf("layer context not found in unit of work")
	}

	_, exists := lCtx.subs.get(topic)
	return exists, nil
}

// QueueLength returns the current number of queued messages
func QueueLength(ctx context.Context) (int, error) {
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return 0, fmt.Errorf("no unit of work found")
	}

	lCtx := getLayerContext(uw)
	if lCtx == nil {
		return 0, fmt.Errorf("layer context not found in unit of work")
	}

	return lCtx.messageQueue.Len(), nil
}
