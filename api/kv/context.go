// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var registryKey = &ctxapi.Key{Name: "kv.provider"}

// WithRegistry attaches a ProviderRegistry to the context so userland code
// (Lua, Go services) can resolve KV spaces by name. Mirrors the topology
// context pattern.
func WithRegistry(ctx context.Context, reg ProviderRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryKey) == nil {
		ac.With(registryKey, reg)
	}
	return ctx
}

// GetRegistry retrieves the ProviderRegistry from ctx, or nil if absent.
func GetRegistry(ctx context.Context) ProviderRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(registryKey); val != nil {
		if reg, ok := val.(ProviderRegistry); ok {
			return reg
		}
	}
	return nil
}
