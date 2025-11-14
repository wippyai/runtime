package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	envservice "github.com/ponyruntime/pony/service/env"
)

func EnvService() boot.Component {
	return boot.New(boot.P{
		Name:      EnvName,
		Phase:     boot.PostInit,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := envservice.NewManager(
				bus,
				dtt,
				logger.Named("env"),
				envservice.NewDefaultEnvStorageFactory(),
			)

			handlers.RegisterListener("env.**", manager)
			return ctx, nil
		},
	})
}
