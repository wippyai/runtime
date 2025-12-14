// Package registry provides a versioned storage system for configuration entries.
package registry

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	registryCtxKey = &ctxapi.Key{Name: "registry.registryCtxKey"}
	finderCtxKey   = &ctxapi.Key{Name: "registry.finderCtxKey"}
	resolverCtxKey = &ctxapi.Key{Name: "registry.resolverCtxKey"}
)

func WithRegistry(ctx context.Context, registry Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryCtxKey) == nil {
		ac.With(registryCtxKey, registry)
	}
	return ctx
}

func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(registryCtxKey); val != nil {
		if reg, ok := val.(Registry); ok {
			return reg
		}
	}
	return nil
}

func WithFinder(ctx context.Context, finder Finder) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(finderCtxKey) == nil {
		ac.With(finderCtxKey, finder)
	}
	return ctx
}

func GetFinder(ctx context.Context) Finder {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(finderCtxKey); val != nil {
		if f, ok := val.(Finder); ok {
			return f
		}
	}
	return nil
}

// WithResolver attaches a DependencyResolver instance to the provided context.
func WithResolver(ctx context.Context, resolver DependencyResolver) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(resolverCtxKey) == nil {
		ac.With(resolverCtxKey, resolver)
	}
	return ctx
}

func GetResolver(ctx context.Context) DependencyResolver {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(resolverCtxKey); val != nil {
		if r, ok := val.(DependencyResolver); ok {
			return r
		}
	}
	return nil
}
