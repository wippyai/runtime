package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pidgen"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/eventbus"
)

func EventBus() boot.Component {
	return boot.New(boot.P{
		Name: EventBusName,
		Load: func(ctx context.Context) (context.Context, error) {
			bus := eventbus.NewBus()
			return event.WithBus(ctx, bus), nil
		},
	})
}

func PIDGen() boot.Component {
	return boot.New(boot.P{
		Name: PIDGenName,
		Load: func(ctx context.Context) (context.Context, error) {
			uniqGen := uniqid.NewGenerator()
			gen := uniqid.NewPIDGenerator(uniqGen, "local")
			return pidgen.WithGenerator(ctx, gen), nil
		},
	})
}
