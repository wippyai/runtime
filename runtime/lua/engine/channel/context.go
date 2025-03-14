package channel

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
)

var uowContext = ctxapi.Key{Name: "channel.context"}

type layerContext struct {
	queue    *engine.TaskQueue
	channels map[*Channel]int // Track named channels with reference counting
}

func getLayerContext(uw engine.UnitOfWork) *layerContext {
	ctx, ok := uw.Values().Get(uowContext)
	if !ok {
		return nil
	}

	if v, ok := ctx.(*layerContext); ok {
		return v
	}

	return nil
}

func ensureLayerContext(uw engine.UnitOfWork) *layerContext {
	if uw == nil {
		return nil
	}

	ctx, ok := uw.Values().Get(uowContext)
	if !ok {
		ctx = &layerContext{
			queue:    engine.NewTaskQueue(),
			channels: make(map[*Channel]int),
		}
		uw.Values().Set(uowContext, ctx)

		return ctx.(*layerContext)
	}

	if v, ok := ctx.(*layerContext); ok {
		return v
	}

	return nil
}

// ActiveChannel represents a channel that currently blocks execution,
// containing its current State and reference information.
type ActiveChannel struct {
	Name  string // Channel identifier
	Slots int    // Available slots in the channel
	Refs  int    // Number of current references
}

// GetActiveChannels returns all channels that currently block execution.
// Each returned ActiveChannel contains the channel's name, available slots,
// and current reference count.
func GetActiveChannels(ctx context.Context) []ActiveChannel {
	uw := engine.GetUnitOfWork(ctx)
	if uw == nil {
		return nil
	}

	r := getLayerContext(uw)
	if r == nil {
		return nil
	}

	result := make([]ActiveChannel, 0, len(r.channels))
	for ch, refs := range r.channels {
		result = append(result, ActiveChannel{
			Name:  ch.name,
			Slots: ch.capacity - ch.size + refs,
			Refs:  refs,
		})
	}

	return result
}
