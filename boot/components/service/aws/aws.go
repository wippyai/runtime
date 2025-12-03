package aws

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/aws/config"
)

func AWS() boot.Component {
	return boot.New(boot.P{
		Name:      AWSConfigName,
		DependsOn: []boot.Name{bootsystem.EnvironmentName},
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
