package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/host"
	"go.uber.org/zap"
)

func Host() boot.Component {
	return boot.New(boot.P{
		Name:      EphemeralHost2Name,
		DependsOn: []boot.Name{system.FactoryName, core.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("host")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			dtt := payload.GetTranscoder(ctx)
			if dtt == nil {
				return ctx, ErrTranscoderNotAvailable
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, ErrHandlerRegistryNotAvailable
			}

			factory := process.GetFactory(ctx)
			if factory == nil {
				return ctx, ErrProcessFactoryNotAvailable
			}

			// Get shared dispatcher registry (contains all registered handlers)
			registry := dispatcherapi.GetRegistry(ctx)
			if registry == nil {
				return ctx, ErrDispatcherRegistryNotAvailable
			}

			// Register process dependency patterns for automatic host dependency resolution
			reg := regapi.GetRegistry(ctx)
			if reg != nil {
				processPatterns := []regapi.DependencyPattern{
					{Path: "data.host", Description: "Reference to a host component"},
					{Path: "data.process", Description: "Reference to a process component"},
				}
				for _, pattern := range processPatterns {
					if err := reg.RegisterDependencyPattern(pattern); err != nil {
						logger.Warn("failed to register process dependency pattern",
							zap.String("path", pattern.Path),
							zap.Error(err))
					}
				}
			}

			manager := host.NewManager(bus, dtt, registry, factory, logger)
			handlers.RegisterListener("process.host", manager)

			logger.Info("host manager registered")
			return ctx, nil
		},
	})
}
