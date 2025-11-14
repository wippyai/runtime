package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/pidgen"
	"github.com/ponyruntime/pony/internal/uniqid"
	"github.com/ponyruntime/pony/system/eventbus"
)

func EventBus() boot.Component {
	return boot.New(boot.P{
		Name:  EventBusName,
		Phase: boot.PreInit,
		Load: func(ctx context.Context) (context.Context, error) {
			bus := eventbus.NewBus()
			return event.WithBus(ctx, bus), nil
		},
	})
}

func PIDGen() boot.Component {
	return boot.New(boot.P{
		Name:  PIDGenName,
		Phase: boot.PreInit,
		Load: func(ctx context.Context) (context.Context, error) {
			uniqGen := uniqid.NewGenerator()
			gen := uniqid.NewPIDGenerator(uniqGen, "local")
			return pidgen.WithGenerator(ctx, gen), nil
		},
	})
}
