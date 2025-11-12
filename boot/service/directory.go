//go:build !plugin_minimal

package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	fsdir "github.com/ponyruntime/pony/service/directory"
)

func Directory() boot.Plugin {
	return boot.New(boot.P{
		Name:      "directory",
		Phase:     boot.PostInit,
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
