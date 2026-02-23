// SPDX-License-Identifier: MPL-2.0

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
			logger := logapi.GetLogger(ctx).Named("router")
			bus := event.GetBus(ctx)
			handlerRegistry := bootpkg.GetHandlerRegistry(ctx)
			readiness := bootpkg.GetReadiness(ctx)

			router, err := eventbus.StartRouter(ctx, bus, eventbus.WithLogger(logger))
			if err != nil {
				return err
			}

			if handlerRegistry == nil {
				logger.Warn("no handler registry found, starting router without handlers")
				return nil
			}

			handlers := handlerRegistry.Handlers()
			logger.Debug("starting event router", zap.Int("handler_count", len(handlers)))

			remaining := 0
			if readiness != nil && len(handlers) > 0 {
				readiness.Add(len(handlers))
				remaining = len(handlers)
				defer func() {
					for remaining > 0 {
						readiness.Done()
						remaining--
					}
				}()
			}

			for _, handler := range handlers {
				if err := router.AddHandler(handler); err != nil {
					return err
				}
				if remaining > 0 {
					readiness.Done()
					remaining--
				}
			}

			return nil
		},
	})
}
