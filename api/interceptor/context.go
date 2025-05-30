package interceptor

import (
	"context"
)

// RegistryContextKey is the key used to store the registry in context
type RegistryContextKey struct{}

// WithInterceptor adds the interceptor registry to the context
func WithInterceptor(ctx context.Context, registry Registry) context.Context {
	return context.WithValue(ctx, RegistryContextKey{}, registry)
}

// GetInterceptor retrieves the interceptor registry from the context
func GetInterceptor(ctx context.Context) Registry {
	if registry, ok := ctx.Value(RegistryContextKey{}).(Registry); ok {
		return registry
	}
	return nil
}
