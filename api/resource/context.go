// Package resource provides a system for managing and accessing shared resources.
package resource

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context key for storing resource-related data
var resourcesCtx = &ctxapi.Key{Name: "resource.registry"}

// WithRegistry attaches a Resource Registry instance to the provided context.
// This allows the Registry to be retrieved later using the GetRegistry function.
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(resourcesCtx) == nil {
		ac.With(resourcesCtx, reg)
	}
	return ctx
}

// GetRegistry retrieves the Resource Registry instance from the provided context.
// Returns nil if no Registry is found in the context.
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if reg := ac.Get(resourcesCtx); reg != nil {
		return reg.(Registry)
	}
	return nil
}
