// SPDX-License-Identifier: MPL-2.0

// Package boot provides application boot and component loading.
package boot

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var configKey = &ctxapi.Key{Name: "boot.config"}

// WithConfig attaches Config to AppContext.
func WithConfig(ctx context.Context, cfg Config) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(configKey) == nil {
		ac.With(configKey, cfg)
	}
	return ctx
}

// GetConfig retrieves Config from AppContext.
// Returns nil if no Config is found.
func GetConfig(ctx context.Context) Config {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if cfg := ac.Get(configKey); cfg != nil {
		if c, ok := cfg.(Config); ok {
			return c
		}
	}
	return nil
}
