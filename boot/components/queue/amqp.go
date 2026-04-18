// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	amqpapi "github.com/wippyai/runtime/api/service/queue/amqp"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/service/queue/amqp"
	"go.uber.org/zap"
)

func AMQP() boot.Component {
	return boot.New(boot.P{
		Name:      AMQPDriverName,
		DependsOn: []boot.Name{ManagerName, bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			if reg := regapi.GetRegistry(ctx); reg != nil {
				pattern := regapi.DependencyPattern{
					Path:          "data.tls.*_env",
					Description:   "Env variables referenced from AMQP TLS config",
					AllowWildcard: true,
				}
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register AMQP TLS dependency pattern", zap.String("path", pattern.Path), zap.Error(err))
				}
			}

			manager := amqp.NewManager(
				bus,
				dtt,
				logger.Named("queue.amqp"),
			)

			handlers.RegisterListener(amqpapi.Kind, manager)
			return ctx, nil
		},
	})
}
