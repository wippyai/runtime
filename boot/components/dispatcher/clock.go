package dispatcher

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/service/dispatcher/clock"
	sysdispatcher "github.com/wippyai/runtime/system/dispatcher"
)

func Clock() boot.Component {
	return boot.New(boot.P{
		Name:      ClockName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := clock.NewService()
			svc.RegisterAll(sysdispatcher.Register)
			return ctx, nil
		},
	})
}
