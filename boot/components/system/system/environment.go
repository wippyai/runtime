package system

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	regapi "github.com/ponyruntime/pony/api/registry"
	bootcore "github.com/ponyruntime/pony/boot/components/core/core"
	"github.com/ponyruntime/pony/system/env"
	"go.uber.org/zap"
)

func Environment() boot.Component {
	var envRegistry *env.Registry

	return boot.New(boot.P{
		Name:      EnvironmentName,
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

			// Register environment dependency patterns
			envPatterns := []regapi.DependencyPattern{
				{Path: "data.env", Description: "Reference to env"},
				{Path: "data.*_env", Description: "Environment variable dependencies", AllowWildcard: true},
				{Path: "meta.*_env", Description: "Environment variable dependencies in metadata", AllowWildcard: true},
			}
			for _, pattern := range envPatterns {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register environment dependency pattern", zap.String("path", pattern.Path), zap.Error(err))
				}
			}

			envRegistry = env.NewRegistry(bus, logger.Named("env"))
			return envapi.WithRegistry(ctx, envRegistry), nil
		},
		Start: func(ctx context.Context) error {
			if envRegistry != nil {
				return envRegistry.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if envRegistry != nil {
				return envRegistry.Stop()
			}
			return nil
		},
	})
}
