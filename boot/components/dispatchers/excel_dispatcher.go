package dispatchers

import (
	"context"
	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/service/excel"
)

const ExcelDefaultWorkers = 4

func Excel() boot.Component {
	var d *excel.Dispatcher

	return boot.New(boot.P{
		Name:      ExcelDispatcherName,
		DependsOn: []boot.ComponentName{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			d = excel.NewDispatcher(ExcelDefaultWorkers)
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
