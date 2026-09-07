// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/service/cdc"
)

const CDCDefaultWorkers = 4

func CDC() boot.Component {
	var d *cdc.Dispatcher

	return boot.New(boot.P{
		Name:      CDCDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			d = cdc.NewDispatcher(cdc.WithWorkers(CDCDefaultWorkers))
			d.RegisterAll(reg.Register)
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			return d.Start(ctx)
		},
		Stop: func(ctx context.Context) error {
			return d.Stop(ctx)
		},
	})
}
