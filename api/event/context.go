package event

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var busCtx = &ctxapi.Key{Name: "bus"}

// WithBus returns a new context with the provided Bus instance attached.
// This allows the Bus to be retrieved later using the GetBus function.
func WithBus(ctx context.Context, bus Bus) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(busCtx) == nil {
		ac.With(busCtx, bus)
	}
	return ctx
}

// GetBus retrieves the Bus instance from the provided context.
// Returns nil if no Bus is found in the context.
func GetBus(ctx context.Context) Bus {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if b := ac.Get(busCtx); b != nil {
		return b.(Bus)
	}
	return nil
}
