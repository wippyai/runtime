// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var networkRegistryKey = &ctxapi.Key{Name: "net.registry"}

// WithNetworkRegistry attaches a NetworkRegistry to the AppContext.
func WithNetworkRegistry(ctx context.Context, reg NetworkRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(networkRegistryKey) == nil {
		ac.With(networkRegistryKey, reg)
	}
	return ctx
}

// GetNetworkRegistry retrieves the NetworkRegistry from the AppContext.
// Returns nil if no NetworkRegistry is found.
func GetNetworkRegistry(ctx context.Context) NetworkRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if reg, ok := ac.Get(networkRegistryKey).(NetworkRegistry); ok {
		return reg
	}
	return nil
}
