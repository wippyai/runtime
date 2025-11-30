package dispatchers

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/service/ws"
)

func WS() boot.Component {
	return boot.New(boot.P{
		Name:      WSDispatcherName,
		DependsOn: []boot.ComponentName{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("dispatcher registrar not found in context")
			}
			svc := ws.NewService()
			svc.RegisterAll(reg.Register)
			return ctx, nil
		},
	})
}
