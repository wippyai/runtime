package dispatcher

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/service/dispatcher/queue"
	sysdispatcher "github.com/wippyai/runtime/system/dispatcher"
)

func Queue() boot.Component {
	return boot.New(boot.P{
		Name:      QueueName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := queue.NewService()
			svc.RegisterAll(sysdispatcher.Register)
			return ctx, nil
		},
	})
}
