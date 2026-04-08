// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/logs"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/relay"
	systempg "github.com/wippyai/runtime/system/pg"
)

func PGDispatcher() boot.Component {
	return boot.New(boot.P{
		Name:      PGDispatcherName,
		DependsOn: []boot.Name{DispatcherName, "pg"},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			pg := pgapi.GetProcessGroups(ctx)
			if pg == nil {
				return ctx, nil
			}

			svc, ok := pg.(*systempg.Service)
			if !ok {
				return ctx, nil
			}

			router := relay.GetRouter(ctx)
			logger := logs.GetLogger(ctx).Named("dispatcher.pg")

			d := systempg.NewDispatcher(svc, router, logger)
			d.RegisterAll(reg.Register)

			return ctx, nil
		},
	})
}
