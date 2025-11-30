package dispatcher

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	sysdispatcher "github.com/wippyai/runtime/system/dispatcher"
	sysfunction "github.com/wippyai/runtime/system/function"
)

func Func() boot.Component {
	return boot.New(boot.P{
		Name:      FuncName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			d := sysfunction.NewDispatcher()
			d.RegisterAll(sysdispatcher.Register)
			return ctx, nil
		},
	})
}
