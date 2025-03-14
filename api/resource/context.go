// Package resource provides a system for managing and accessing shared resources.
package resource

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context key for storing resource-related data
var resourcesCtx = &ctxapi.Key{Name: "resource.registry"} //nolint:gochecknoglobals

// WithResources attaches a Resource Registry instance to the provided context.
// This allows the Registry to be retrieved later using the GetResources function.
func WithResources(ctx context.Context, reg Registry) context.Context {
	return context.WithValue(ctx, resourcesCtx, reg)
}

// GetResources retrieves the Resource Registry instance from the provided context.
// Returns nil if no Registry is found in the context.
func GetResources(ctx context.Context) Registry {
	if reg, ok := ctx.Value(resourcesCtx).(Registry); ok {
		return reg
	}
	return nil
}
