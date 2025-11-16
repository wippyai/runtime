package core

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/supervisor"
	"go.uber.org/zap"
)

func Supervisor() boot.Component {
	var sup *supervisor.Supervisor

	return boot.New(boot.P{
		Name:      SupervisorName,
		Phase:     boot.Init,
		DependsOn: []boot.ComponentName{RegistryName},
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

			// Register lifecycle dependency pattern
			if err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
				Path:          "data.lifecycle.depends_on",
				Description:   "Lifecycle dependencies",
				AllowWildcard: true,
			}); err != nil {
				logger.Warn("failed to register lifecycle dependency pattern", zap.Error(err))
			}

			sup = supervisor.NewSupervisor(bus, logger.Named("core"))
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if sup != nil {
				return sup.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if sup != nil {
				return sup.Stop()
			}
			return nil
		},
	})
}
