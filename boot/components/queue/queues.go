// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

func Queues() boot.Component {
	return boot.New(boot.P{
		Name: QueuesName,
		DependsOn: []boot.Name{
			bootcore.RegistryName,
			ManagerName,
		},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("queue.queues")
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			queueManager := queueapi.GetManager(ctx)
			reg := regapi.GetRegistry(ctx)

			queuePatterns := []regapi.DependencyPattern{
				{Path: "data.driver", Description: "Reference to queue driver"},
				{Path: "data.dead_letter.queue", Description: "Reference to dead-letter queue"},
			}
			for _, pattern := range queuePatterns {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register queue dependency pattern", zap.String("path", pattern.Path), zap.Error(err))
				}
			}

			handler := queuesvc.NewDeclarationHandler(
				bus,
				queueManager,
				dtt,
				logger,
			)

			handlers.RegisterListener("queue.queue", handler)
			return ctx, nil
		},
	})
}
