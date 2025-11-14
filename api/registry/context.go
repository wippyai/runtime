// Package registry provides a versioned storage system for configuration entries.
package registry

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// Context keys for storing registry-related data
var (
	registryCtx = &ctxapi.Key{Name: "registry.registry"}
	finderCtx   = &ctxapi.Key{Name: "registry.finder"}
	resolverCtx = &ctxapi.Key{Name: "registry.resolver"}
)

// WithRegistry attaches a Registry instance to the provided context.
// This allows the Registry to be retrieved later using the GetRegistry function.
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

// GetRegistry retrieves the Registry instance from the provided context.
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

// WithFinder attaches a Finder instance to the provided context.
// This allows the Finder to be retrieved later using the GetFinder function.
func WithFinder(ctx context.Context, finder Finder) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(finderCtx) == nil {
		ac.With(finderCtx, finder)
	}
	return ctx
}

// GetFinder retrieves the Finder instance from the provided context.
// Returns nil if no Finder is found in the context.
func GetFinder(ctx context.Context) Finder {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if f := ac.Get(finderCtx); f != nil {
		return f.(Finder)
	}
	return nil
}

// WithResolver attaches a DependencyResolver instance to the provided context.
// This allows the DependencyResolver to be retrieved later using the GetResolver function.
func WithResolver(ctx context.Context, resolver DependencyResolver) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(resolverCtx) == nil {
		ac.With(resolverCtx, resolver)
	}
	return ctx
}

// GetResolver retrieves the DependencyResolver instance from the provided context.
// Returns nil if no DependencyResolver is found in the context.
func GetResolver(ctx context.Context) DependencyResolver {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if r := ac.Get(resolverCtx); r != nil {
		return r.(DependencyResolver)
	}
	return nil
}
