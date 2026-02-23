// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var registryKey = &ctxapi.Key{Name: "fs.registry"}

// WithRegistry returns a new context with the provided filesystem Registry attached.
// This allows the Registry to be retrieved later using the GetRegistry function.
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

// GetRegistry retrieves the filesystem Registry instance from the provided context.
// Returns nil if no Registry is found in the context.
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
