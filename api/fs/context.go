// Package fs provides filesystem abstractions and a registry for managing
// multiple filesystem instances.
package fs

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

var registryCtx = &ctxapi.Key{Name: "fs.registry"}

// WithRegistry returns a new context with the provided filesystem Registry attached.
// This allows the Registry to be retrieved later using the GetRegistry function.
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryCtx) == nil {
		ac.With(registryCtx, reg)
	}
	return ctx
}

// GetRegistry retrieves the filesystem Registry instance from the provided context.
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
