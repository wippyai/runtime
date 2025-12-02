package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/dispatcher"
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
			return process2.WithPIDGenerator(ctx, gen), nil
		},
	})
}

func Dispatcher() boot.Component {
	return boot.New(boot.P{
		Name: DispatcherName,
		Load: func(ctx context.Context) (context.Context, error) {
			// Create dispatcher registry for this application instance
			reg := dispatcher.NewRegistry()
			if err := dispatcherapi.WithRegistry(ctx, reg); err != nil {
				return ctx, err
			}
			return ctx, nil
		},
	})
}
