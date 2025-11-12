package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/system/env"
)

func Environment() boot.Plugin {
	var envRegistry *env.Registry

	return boot.New(boot.P{
		Name:      EnvironmentName,
		Phase:     boot.Init,
		DependsOn: []string{"eventbus", "logger"},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)

			envRegistry = env.NewRegistry(bus, logger.Named("env"))
			return envapi.WithRegistry(ctx, envRegistry), nil
		},
		Start: func(ctx context.Context) error {
			if envRegistry != nil {
				return envRegistry.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if envRegistry != nil {
				return envRegistry.Stop()
			}
			return nil
		},
	})
}
