// Package registry provides a versioned storage system for configuration entries.
package registry

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context key for storing registry-related data
var registryCtx = &ctxapi.Key{Name: "registry.registry"} //nolint:gochecknoglobals

// WithRegistry attaches a Registry instance to the provided context.
// This allows the Registry to be retrieved later using the GetRegistry function.
func WithRegistry(ctx context.Context, registry Registry) context.Context {
	return context.WithValue(ctx, registryCtx, registry)
}

// GetRegistry retrieves the Registry instance from the provided context.
// Returns nil if no Registry is found in the context.
func GetRegistry(ctx context.Context) Registry {
	if reg, ok := ctx.Value(registryCtx).(Registry); ok {
		return reg
	}
	return nil
}
