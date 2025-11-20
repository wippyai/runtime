package queue

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// Context keys for queue system
var (
	managerKey             = &ctxapi.Key{Name: "queue.manager"}
	publishChainKey        = &ctxapi.Key{Name: "queue.publish_chain"}
	interceptorRegistryKey = &ctxapi.Key{Name: "queue.interceptor_registry"}
	deliveryKey            = &ctxapi.Key{Name: "queue.delivery", Inherit: true}
)

// WithManager attaches a queue manager to the context
func WithManager(ctx context.Context, mgr Manager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(managerKey) == nil {
		ac.With(managerKey, mgr)
	}
	return ctx
}

// GetManager retrieves the queue manager from context
func GetManager(ctx context.Context) Manager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(managerKey); val != nil {
		if mgr, ok := val.(Manager); ok {
			return mgr
		}
	}
	return nil
}

// WithDelivery attaches a delivery to the frame context
func WithDelivery(ctx context.Context, delivery *Delivery) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(deliveryKey, delivery)
}

// GetDelivery retrieves the current delivery from frame context
func GetDelivery(ctx context.Context) (*Delivery, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}
	if val, ok := fc.Get(deliveryKey); ok {
		if delivery, ok := val.(*Delivery); ok {
			return delivery, true
		}
	}
	return nil, false
}

// DeliveryPair creates a context pair for delivery
func DeliveryPair(delivery *Delivery) ctxapi.Pair {
	return ctxapi.Pair{Key: deliveryKey, Value: delivery}
}

// WithPublishChain attaches a publish chain to the context
func WithPublishChain(ctx context.Context, chain PublishChain) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(publishChainKey) == nil {
		ac.With(publishChainKey, chain)
	}
	return ctx
}

// GetPublishChain retrieves the publish chain from context
func GetPublishChain(ctx context.Context) PublishChain {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(publishChainKey); val != nil {
		if chain, ok := val.(PublishChain); ok {
			return chain
		}
	}
	return nil
}

// WithPublishInterceptorRegistry attaches a publish interceptor registry to the context
func WithPublishInterceptorRegistry(ctx context.Context, registry PublishInterceptorRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(interceptorRegistryKey) == nil {
		ac.With(interceptorRegistryKey, registry)
	}
	return ctx
}

// GetPublishInterceptorRegistry retrieves the publish interceptor registry from context
func GetPublishInterceptorRegistry(ctx context.Context) PublishInterceptorRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(interceptorRegistryKey); val != nil {
		if reg, ok := val.(PublishInterceptorRegistry); ok {
			return reg
		}
	}
	return nil
}
