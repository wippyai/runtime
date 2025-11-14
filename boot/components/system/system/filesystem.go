package system

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	logapi "github.com/ponyruntime/pony/api/logs"
	regapi "github.com/ponyruntime/pony/api/registry"
	bootcore "github.com/ponyruntime/pony/boot/components/core/core"
	"github.com/ponyruntime/pony/system/fs"
	"go.uber.org/zap"
)

func Filesystem() boot.Component {
	var fsRegistry *fs.Registry

	return boot.New(boot.P{
		Name:      FilesystemName,
		Phase:     boot.Init,
		DependsOn: []boot.ComponentName{bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available in context")
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("registry not available in context")
			}

			// Register filesystem dependency pattern
			if err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
				Path:        "data.fs",
				Description: "Reference to filesystem",
			}); err != nil {
				logger.Warn("failed to register filesystem dependency pattern", zap.Error(err))
			}

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
