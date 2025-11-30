package dispatcher

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	sysfunction "github.com/wippyai/runtime/system/function"
)

func Func() boot.Component {
	return boot.New(boot.P{
		Name:      FuncName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("dispatcher registrar not found in context")
			}
			d := sysfunction.NewDispatcher()
			d.RegisterAll(reg.Register)
			return ctx, nil
		},
	})
}
