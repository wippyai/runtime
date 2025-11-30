package dispatchers

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/service/exec"
)

func Exec() boot.Component {
	return boot.New(boot.P{
		Name:      ExecDispatcherName,
		DependsOn: []boot.ComponentName{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("dispatcher registrar not found in context")
			}
			svc := exec.NewDispatcherService()
			svc.RegisterAll(reg.Register)
			return ctx, nil
		},
	})
}
