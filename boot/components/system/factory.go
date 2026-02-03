package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	api "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/system/process"
)

const FactoryName = "system.factory"

func Factory() boot.Component {
	return boot.New(boot.P{
		Name: FactoryName,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("factory")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			registry := process.NewFactoryRegistry(bus, logger)
			if err := registry.Start(ctx); err != nil {
				return ctx, NewFactoryStartError(err)
			}

			api.WithFactory(ctx, registry)

			logger.Info("factory registry started")
			return ctx, nil
		},
	})
}
