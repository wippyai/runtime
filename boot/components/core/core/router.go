package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

func EventRouter() boot.Component {
	return boot.New(boot.P{
		Name:      EventRouterName,
		Phase:     boot.Init,
		DependsOn: []boot.ComponentName{RegistryName},
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
