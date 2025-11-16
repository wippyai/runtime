package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	prochost "github.com/wippyai/runtime/service/host"
)

func Host() boot.Component {
	return boot.New(boot.P{
		Name:      EphemeralHostName,
		Phase:     boot.PostInit,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := prochost.NewHostManager(
				bus,
				dtt,
				logger.Named("hosts"),
			)

			handlers.RegisterListener("process.host", manager)
			return ctx, nil
		},
	})
}
