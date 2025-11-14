package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	bootsystem "github.com/ponyruntime/pony/boot/components/system/system"
	"github.com/ponyruntime/pony/service/aws/config"
)

func AWS() boot.Component {
	return boot.New(boot.P{
		Name:      AWSConfigName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{bootsystem.EnvironmentName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			envRegistry := envapi.GetRegistry(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := config.NewManager(
				bus,
				dtt,
				logger.Named("config.aws"),
				envRegistry,
			)

			handlers.RegisterListener("config.aws", manager)
			return ctx, nil
		},
	})
}
