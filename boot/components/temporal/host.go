package temporal

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/service/temporal/host"
)

func HostManager() boot.Component {
	return boot.New(boot.P{
		Name: HostManagerName,
		DependsOn: []boot.ComponentName{
			ClientManagerName,
		},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available in context")
			}

			resources := resource.GetRegistry(ctx)
			if resources == nil {
				return ctx, fmt.Errorf("resource registry not available in context")
			}

			manager := host.NewManager(ctx, bus, resources, logger.Named("temporal.host"))
			if err := manager.Start(ctx); err != nil {
				return ctx, fmt.Errorf("failed to start host manager: %w", err)
			}

			logger.Info("temporal host manager initialized")
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal host manager started")
			return nil
		},
		Stop: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal host manager stopped")
			return nil
		},
	})
}
