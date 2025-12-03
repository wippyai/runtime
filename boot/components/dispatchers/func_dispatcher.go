package dispatchers

import (
	"context"
	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	sysfunction "github.com/wippyai/runtime/system/function"
)

func Func() boot.Component {
	return boot.New(boot.P{
		Name:      FuncDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}
			d := sysfunction.NewDispatcher()
			d.RegisterAll(reg.Register)
			return ctx, nil
		},
	})
}
