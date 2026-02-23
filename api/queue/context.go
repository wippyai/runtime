// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	managerKey  = &ctxapi.Key{Name: "queue.manager"}
	deliveryKey = &ctxapi.Key{Name: "queue.delivery", Inherit: true}
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
