package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func EventRouter() boot.Component {
	return boot.New(boot.P{
		Name:      EventRouterName,
		DependsOn: []boot.Name{RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlerRegistry := bootpkg.GetHandlerRegistry(ctx)

			if handlerRegistry == nil {
				logger.Warn("no handler registry found, starting router without handlers")
				_, err := eventbus.StartRouter(ctx, bus)
				return err
			}

			handlers := handlerRegistry.Handlers()
			logger.Debug("starting event router", zap.Int("handler_count", len(handlers)))

			_, err := eventbus.StartRouter(ctx, bus, eventbus.WithHandlers(handlers...))
			return err
		},
	})
}
