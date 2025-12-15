package resource

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var registryKey = &ctxapi.Key{Name: "resource.registry"}

// WithRegistry attaches a Registry to the context.
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryKey) == nil {
		ac.With(registryKey, reg)
	}
	return ctx
}

// GetRegistry retrieves the Registry from the context.
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if reg := ac.Get(registryKey); reg != nil {
		return reg.(Registry)
	}
	return nil
}
