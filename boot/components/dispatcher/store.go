package dispatcher

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/service/dispatcher/store"
	sysdispatcher "github.com/wippyai/runtime/system/dispatcher"
)

func Store() boot.Component {
	return boot.New(boot.P{
		Name:      StoreName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := store.NewService()
			svc.RegisterAll(sysdispatcher.Register)
			return ctx, nil
		},
	})
}
