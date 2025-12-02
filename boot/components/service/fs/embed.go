package fs

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/fs/embed"
)

func Embed() boot.Component {
	return boot.New(boot.P{
		Name:      EmbedName,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			// Get embed registry from context (set by run-pack)
			embedReg := embedapi.GetRegistry(ctx)
			if embedReg == nil {
				// No embed registry - skip registration
				// This happens when running without packs
				logger.Debug("embed registry not found, skipping embed filesystem support")
				return ctx, nil
			}

			manager := embed.NewManager(
				bus,
				dtt,
				embedReg,
				logger.Named("fs.embed"),
			)

			handlers.RegisterListener("fs.embed", manager)
			logger.Info("embed filesystem support enabled")
			return ctx, nil
		},
	})
}
