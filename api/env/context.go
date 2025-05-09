// Package env provides access to environment variables with flexible storage backends.
package env

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context key for the environment registry
var registryCtx = &ctxapi.Key{Name: "env.registry"}

// WithRegistry returns a new context with the provided Registry attached
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	return context.WithValue(ctx, registryCtx, reg)
}

// GetRegistry retrieves the environment registry from the context
func GetRegistry(ctx context.Context) Registry {
	if reg, ok := ctx.Value(registryCtx).(Registry); ok {
		return reg
	}
	return nil
}
