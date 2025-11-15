// Package interceptor provides request and operation interception.
package interceptor

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// Context keys for storing interceptor-related data
var (
	chainCtx    = &ctxapi.Key{Name: "interceptor.chain"}
	registryCtx = &ctxapi.Key{Name: "interceptor.registry"}
)

// WithChain adds the interceptor chain to the context
func WithChain(ctx context.Context, chain Chain) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(chainCtx) == nil {
		ac.With(chainCtx, chain)
	}
	return ctx
}

// GetChain retrieves the interceptor chain from the context
func GetChain(ctx context.Context) Chain {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(chainCtx); val != nil {
		if chain, ok := val.(Chain); ok {
			return chain
		}
	}
	return nil
}

// WithRegistry adds the interceptor registry to the context
func WithRegistry(ctx context.Context, registry Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryCtx) == nil {
		ac.With(registryCtx, registry)
	}
	return ctx
}

// GetRegistry retrieves the interceptor registry from the context
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(registryCtx); val != nil {
		if registry, ok := val.(Registry); ok {
			return registry
		}
	}
	return nil
}
