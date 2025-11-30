package fs

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	fsdir "github.com/wippyai/runtime/service/fs/directory"
)

func Directory() boot.Component {
	return boot.New(boot.P{
		Name:      DirectoryName,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := fsdir.NewDirectoryManager(
				bus,
				dtt,
				nil,
				logger.Named("fs.dir"),
			)

			handlers.RegisterListener("fs.directory", manager)
			return ctx, nil
		},
	})
}
