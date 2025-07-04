package interceptor

import (
	"context"
)

// RegistryContextKey is the key used to store the registry in context
type RegistryContextKey struct{}

type OptionsContextKey struct{}

type CancelContextKey struct{}

// WithInterceptor adds the interceptor registry to the context
func WithInterceptor(ctx context.Context, registry Registry) context.Context {
	return context.WithValue(ctx, RegistryContextKey{}, registry)
}

// GetInterceptors retrieves the interceptor registry from the context
func GetInterceptors(ctx context.Context) Registry {
	if registry, ok := ctx.Value(RegistryContextKey{}).(Registry); ok {
		return registry
	}
	return nil
}

func GetOptionsFromContext(ctx context.Context) Options {
	if options, ok := ctx.Value(OptionsContextKey{}).(Options); ok {
		return options
	}

	return Options{}
}

func WithOptions(ctx context.Context, options Options) context.Context {
	return context.WithValue(ctx, OptionsContextKey{}, options)
}

func WithCancel(ctx context.Context, cancel context.CancelFunc) context.Context {
	return context.WithValue(ctx, CancelContextKey{}, cancel)
}

func GetCancelFromContext(ctx context.Context) context.CancelFunc {
	if cancel, ok := ctx.Value(CancelContextKey{}).(context.CancelFunc); ok {
		return cancel
	}
	return nil
}
