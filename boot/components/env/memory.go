package env

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	envmemory "github.com/wippyai/runtime/service/env/memory"
)

func Memory() boot.Component {
	return boot.New(boot.P{
		Name:      MemoryName,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := envmemory.NewManager(
				bus,
				dtt,
				logger.Named("env.memory"),
			)

			handlers.RegisterListener("env.storage.memory", manager)
			return ctx, nil
		},
	})
}
