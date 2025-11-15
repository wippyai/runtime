package cli

import (
	"context"

	"github.com/wippyai/runtime/boot/deps/client"
)

// RegistryClient context helpers for CLI commands
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
