package dispatcher

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/service/dispatcher/stream"
)

func Stream() boot.Component {
	return boot.New(boot.P{
		Name:      StreamName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("dispatcher registrar not found in context")
			}
			svc := stream.NewService()
			svc.RegisterAll(reg.Register)
			return ctx, nil
		},
	})
}
