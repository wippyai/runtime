package cli

import (
	"context"

	"github.com/ponyruntime/pony/deps/client"
)

// RegistryClient context helpers
type registryClientKey struct{}

func WithRegistryClient(ctx context.Context, client *client.RegistryClient) context.Context {
	return context.WithValue(ctx, registryClientKey{}, client)
}

func GetRegistryClient(ctx context.Context) *client.RegistryClient {
	if v := ctx.Value(registryClientKey{}); v != nil {
		return v.(*client.RegistryClient)
	}
	return nil
}

// Loader context helpers
type loaderKey struct{}

func WithLoader(ctx context.Context, ldr interface{}) context.Context {
	return context.WithValue(ctx, loaderKey{}, ldr)
}

func GetLoader(ctx context.Context) interface{} {
	if v := ctx.Value(loaderKey{}); v != nil {
		return v
	}
	return nil
}
