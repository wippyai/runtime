// Package registry provides a versioned storage system for configuration entries.
package registry

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	registryKey = &ctxapi.Key{Name: "registry"}
	finderKey   = &ctxapi.Key{Name: "registry.finder"}
	resolverKey = &ctxapi.Key{Name: "registry.resolver"}
)

func WithRegistry(ctx context.Context, registry Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryKey) == nil {
		ac.With(registryKey, registry)
	}
	return ctx
}

func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(registryKey); val != nil {
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
	if ac.Get(finderKey) == nil {
		ac.With(finderKey, finder)
	}
	return ctx
}

func GetFinder(ctx context.Context) Finder {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(finderKey); val != nil {
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
	if ac.Get(resolverKey) == nil {
		ac.With(resolverKey, resolver)
	}
	return ctx
}

func GetResolver(ctx context.Context) DependencyResolver {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(resolverKey); val != nil {
		if r, ok := val.(DependencyResolver); ok {
			return r
		}
	}
	return nil
}
