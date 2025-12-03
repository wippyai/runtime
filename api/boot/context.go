// Package boot provides application boot and component loading.
package boot

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var configCtxKey = &ctxapi.Key{Name: "boot.config"}

// WithConfig attaches Config to AppContext.
func WithConfig(ctx context.Context, cfg Config) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(configCtxKey) == nil {
		ac.With(configCtxKey, cfg)
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
	if cfg := ac.Get(configCtxKey); cfg != nil {
		if c, ok := cfg.(Config); ok {
			return c
		}
	}
	return nil
}
