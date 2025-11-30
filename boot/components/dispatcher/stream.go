package dispatcher

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/service/dispatcher/stream"
	sysdispatcher "github.com/wippyai/runtime/system/dispatcher"
)

func Stream() boot.Component {
	return boot.New(boot.P{
		Name:      StreamName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := stream.NewService()
			svc.RegisterAll(sysdispatcher.Register)
			return ctx, nil
		},
	})
}
