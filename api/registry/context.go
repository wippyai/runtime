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

// WithRegistry attaches a Registry instance to the provided context.
// This allows the Registry to be retrieved later using the GetRegistry function.
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

// GetRegistry retrieves the Registry instance from the provided context.
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if reg := ac.Get(registryCtxKey); reg != nil {
		return reg.(Registry)
	}
	return nil
}

// WithFinder attaches a Finder instance to the provided context.
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

// GetFinder retrieves the Finder instance from the provided context.
func GetFinder(ctx context.Context) Finder {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if f := ac.Get(finderCtxKey); f != nil {
		return f.(Finder)
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

// GetResolver retrieves the DependencyResolver instance from the provided context.
func GetResolver(ctx context.Context) DependencyResolver {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if r := ac.Get(resolverCtxKey); r != nil {
		return r.(DependencyResolver)
	}
	return nil
}
