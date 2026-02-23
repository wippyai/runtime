// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
)

func Events() boot.Component {
	var d *eventbus.Dispatcher

	return boot.New(boot.P{
		Name:      EventsDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, nil
			}

			node := relay.GetNode(ctx)
			if node == nil {
				return ctx, nil
			}

			d = eventbus.NewDispatcher(bus, node)
			d.RegisterAll(reg.Register)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if d == nil {
				return nil
			}
			return d.Start(ctx)
		},
		Stop: func(ctx context.Context) error {
			if d == nil {
				return nil
			}
			return d.Stop(ctx)
		},
	})
}
