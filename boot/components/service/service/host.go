//go:build !plugin_minimal

package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	prochost "github.com/ponyruntime/pony/service/host"
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
