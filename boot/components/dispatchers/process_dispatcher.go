// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	sysprocess "github.com/wippyai/runtime/system/process"
)

func Process() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			manager := process.GetManager(ctx)
			router := relay.GetRouter(ctx)
			topo := topology.GetTopology(ctx)
			logger := logs.GetLogger(ctx).Named("dispatcher.process")

			d := sysprocess.NewDispatcher(manager, router, topo, logger)
			d.RegisterAll(reg.Register)

			return ctx, nil
		},
	})
}
