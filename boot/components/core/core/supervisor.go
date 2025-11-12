package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/system/supervisor"
)

func Supervisor() boot.Component {
	var sup *supervisor.Supervisor

	return boot.New(boot.P{
		Name:      SupervisorName,
		Phase:     boot.Init,
		DependsOn: []string{LoggerName, EventBusName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)

			sup = supervisor.NewSupervisor(bus, logger.Named("core"))
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if sup != nil {
				return sup.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if sup != nil {
				return sup.Stop()
			}
			return nil
		},
	})
}
