// Package function provides abstractions for managing and executing asynchronous functions.
package function

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	registryCtx            = &ctxapi.Key{Name: "functions.registry"}
	interceptorChainCtx    = &ctxapi.Key{Name: "interceptor.chain"}
	interceptorRegistryCtx = &ctxapi.Key{Name: "interceptor.registry"}
)

// WithRegistry returns a new context with the provided function Registry attached.
// This allows the Registry to be retrieved later using the GetRegistry function.
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryCtx) == nil {
		ac.With(registryCtx, reg)
	}
	return ctx
}

// GetRegistry retrieves the function registry from the provided context.
// Returns nil if no Registry is found in the context.
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if reg := ac.Get(registryCtx); reg != nil {
		return reg.(Registry)
	}
	return nil
}

// WithInterceptorChain adds the interceptor chain to the context
func WithInterceptorChain(ctx context.Context, chain InterceptorChain) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(interceptorChainCtx) == nil {
		ac.With(interceptorChainCtx, chain)
	}
	return ctx
}

// GetInterceptorChain retrieves the interceptor chain from the context
func GetInterceptorChain(ctx context.Context) InterceptorChain {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(interceptorChainCtx); val != nil {
		if chain, ok := val.(InterceptorChain); ok {
			return chain
		}
	}
	return nil
}

// WithInterceptorRegistry adds the interceptor registry to the context
func WithInterceptorRegistry(ctx context.Context, registry InterceptorRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(interceptorRegistryCtx) == nil {
		ac.With(interceptorRegistryCtx, registry)
	}
	return ctx
}

// GetInterceptorRegistry retrieves the interceptor registry from the context
func GetInterceptorRegistry(ctx context.Context) InterceptorRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(interceptorRegistryCtx); val != nil {
		if registry, ok := val.(InterceptorRegistry); ok {
			return registry
		}
	}
	return nil
}
