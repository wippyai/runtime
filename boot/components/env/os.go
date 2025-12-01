package env

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	envos "github.com/wippyai/runtime/service/env/os"
)

type OSOptions struct {
	StaticEnv map[string]string
}

func OS(opts ...func(*OSOptions)) boot.Component {
	options := &OSOptions{}
	for _, opt := range opts {
		opt(options)
	}

	return boot.New(boot.P{
		Name:      OSName,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			var managerOpts []envos.ManagerOption
			if options.StaticEnv != nil {
				managerOpts = append(managerOpts, envos.WithStaticEnv(options.StaticEnv))
			}

			manager := envos.NewManager(
				bus,
				dtt,
				logger.Named("env.os"),
				managerOpts...,
			)

			handlers.RegisterListener("env.storage.os", manager)
			return ctx, nil
		},
	})
}
