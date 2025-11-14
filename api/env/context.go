// Package env provides access to environment variables with flexible storage backends.
package env

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var registryCtxKey = &ctxapi.Key{Name: "env.registry"}

// WithRegistry returns a new context with the provided Registry attached
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryCtxKey) == nil {
		ac.With(registryCtxKey, reg)
	}
	return ctx
}

// GetRegistry retrieves the environment registry from the context
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
