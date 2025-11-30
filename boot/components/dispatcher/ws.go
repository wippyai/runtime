package dispatcher

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/service/dispatcher/ws"
)

func WS() boot.Component {
	return boot.New(boot.P{
		Name:      WSName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
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
