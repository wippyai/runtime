package env

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsys "github.com/wippyai/runtime/boot/components/system"
	envfile "github.com/wippyai/runtime/service/env/file"
)

func File() boot.Component {
	return boot.New(boot.P{
		Name:      FileName,
		DependsOn: []boot.Name{bootsys.EnvironmentName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := envfile.NewManager(
				bus,
				dtt,
				logger.Named("env.file"),
			)

			handlers.RegisterListener("env.storage.file", manager)
			return ctx, nil
		},
	})
}
