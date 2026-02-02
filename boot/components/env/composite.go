package env

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsys "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/env/composite"
)

func Composite() boot.Component {
	return boot.New(boot.P{
		Name:      CompositeName,
		DependsOn: []boot.Name{MemoryName, FileName, OSName, bootsys.EnvironmentName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := composite.NewManager(
				bus,
				dtt,
				logger.Named("env.composite"),
			)

			handlers.RegisterListener("env.storage.router", manager)
			return ctx, nil
		},
	})
}
