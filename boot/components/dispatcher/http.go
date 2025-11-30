package dispatcher

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	httpclient "github.com/wippyai/runtime/service/http/client"
)

func HTTP() boot.Component {
	return boot.New(boot.P{
		Name:      HTTPName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("dispatcher registrar not found in context")
			}
			svc := httpclient.NewService()
			svc.RegisterAll(reg.Register)
			return ctx, nil
		},
	})
}
