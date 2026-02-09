package net

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var serviceKey = &ctxapi.Key{Name: "net.service"}

// WithService attaches a network Service to the AppContext.
func WithService(ctx context.Context, svc Service) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(serviceKey) == nil {
		ac.With(serviceKey, svc)
	}
	return ctx
}

// GetService retrieves the network Service from the AppContext.
// Returns nil if no Service is found.
func GetService(ctx context.Context) Service {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if svc, ok := ac.Get(serviceKey).(Service); ok {
		return svc
	}
	return nil
}
