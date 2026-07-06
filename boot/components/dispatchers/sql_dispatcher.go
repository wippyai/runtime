// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/service/sql"
)

func SQL() boot.Component {
	return boot.New(boot.P{
		Name:      SQLDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}
			svc := sql.NewDispatcher()
			svc.SetCollector(metricsapi.GetCollector(ctx))
			svc.RegisterAll(reg.Register)
			return ctx, nil
		},
	})
}
