// Package fs provides filesystem abstractions and a registry for managing
// multiple filesystem instances.
package fs

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
)

var registryCtx = &ctxapi.Key{Name: "fs.registry"} //nolint:gochecknoglobals

// WithFSRegistry returns a new context with the provided filesystem Registry attached.
// This allows the Registry to be retrieved later using the GetRegistry function.
func WithFSRegistry(ctx context.Context, reg Registry) context.Context {
	return context.WithValue(ctx, registryCtx, reg)
}

// GetRegistry retrieves the filesystem Registry instance from the provided context.
// Returns nil if no Registry is found in the context.
func GetRegistry(ctx context.Context) Registry {
	if reg, ok := ctx.Value(registryCtx).(Registry); ok {
		return reg
	}

	return nil
}
