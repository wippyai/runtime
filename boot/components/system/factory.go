package system

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	api "github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/system/process2"
)

const FactoryName = "system.factory"

func Factory() boot.Component {
	return boot.New(boot.P{
		Name: FactoryName,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available")
			}

			registry := process2.NewFactoryRegistry(bus, logger)
			if err := registry.Start(ctx); err != nil {
				return ctx, fmt.Errorf("failed to start factory registry: %w", err)
			}

			api.WithFactory(ctx, registry)

			logger.Info("factory registry started")
			return ctx, nil
		},
	})
}
