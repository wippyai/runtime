// Package function provides abstractions for managing and executing asynchronous functions.
package function

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
)

var registryCtx = &ctxapi.Key{Name: "functions.registry"} //nolint:gochecknoglobals

// WithFunctions returns a new context with the provided function Registry attached.
// This allows the Registry to be retrieved later using the GetRegistry function.
func WithFunctions(ctx context.Context, reg Registry) context.Context {
	return context.WithValue(ctx, registryCtx, reg)
}

// GetRegistry retrieves the function registry from the provided context.
// Returns nil if no Registry is found in the context.
func GetRegistry(ctx context.Context) Registry {
	if reg, ok := ctx.Value(registryCtx).(Registry); ok {
		return reg
	}

	return nil
}
