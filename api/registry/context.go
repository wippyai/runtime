// Package registry provides a versioned storage system for configuration entries.
package registry

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context key for storing registry-related data
var registryCtx = &ctxapi.Key{Name: "registry.registry", Scope: ctxapi.ScopeThread}

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
