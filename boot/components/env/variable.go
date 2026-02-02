package env

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	envsvc "github.com/wippyai/runtime/service/env"
)

func Variable() boot.Component {
	return boot.New(boot.P{
		Name:      VariableName,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := envsvc.NewVariableManager(
				bus,
				dtt,
				logger.Named("env.variable"),
			)

			handlers.RegisterListener("env.variable", manager)
			return ctx, nil
		},
	})
}
