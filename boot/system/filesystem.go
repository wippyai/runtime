package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/system/fs"
)

func Filesystem() boot.Plugin {
	var fsRegistry *fs.Registry

	return boot.New(boot.P{
		Name:      FilesystemName,
		Phase:     boot.Init,
		DependsOn: []string{"eventbus", "logger"},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)

			fsRegistry = fs.NewFSRegistry(bus, logger.Named("fs"))
			return fsapi.WithRegistry(ctx, fsRegistry), nil
		},
		Start: func(ctx context.Context) error {
			if fsRegistry != nil {
				return fsRegistry.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if fsRegistry != nil {
				return fsRegistry.Stop()
			}
			return nil
		},
	})
}
