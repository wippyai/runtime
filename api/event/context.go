package event

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

var busCtx = &ctxapi.Key{Name: "bus"}

// WithBus returns a new context with the provided Bus instance attached.
// This allows the Bus to be retrieved later using the GetBus function.
func WithBus(ctx context.Context, bus Bus) context.Context {
	return context.WithValue(ctx, busCtx, bus)
}

// GetBus retrieves the Bus instance from the provided context.
// Returns nil if no Bus is found in the context.
func GetBus(ctx context.Context) Bus {
	if b, ok := ctx.Value(busCtx).(Bus); ok {
		return b
	}

	return nil
}
