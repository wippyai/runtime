package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/system/fs"
	"go.uber.org/zap"
)

func Filesystem() boot.Component {
	var fsRegistry *fs.Registry

	return boot.New(boot.P{
		Name:      FilesystemName,
		DependsOn: []boot.Name{bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("fs")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, ErrRegistryNotAvailable
			}

			// Register filesystem dependency pattern
			if err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
				Path:        "data.fs",
				Description: "Reference to filesystem",
			}); err != nil {
				logger.Warn("failed to register filesystem dependency pattern", zap.Error(err))
			}

			fsRegistry = fs.NewRegistry(bus, logger.Named("fs"))
			return fsapi.WithRegistry(ctx, fsRegistry), nil
		},
		Start: func(ctx context.Context) error {
			if fsRegistry != nil {
				return fsRegistry.Start(ctx)
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if fsRegistry != nil {
				return fsRegistry.Stop()
			}
			return nil
		},
	})
}
