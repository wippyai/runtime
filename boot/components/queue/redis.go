// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	redisapi "github.com/wippyai/runtime/api/service/queue/redis"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/queue/redis"
)

func Redis() boot.Component {
	return boot.New(boot.P{
		Name:      RedisDriverName,
		DependsOn: []boot.Name{ManagerName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := redis.NewManager(
				bus,
				dtt,
				logger.Named("queue.redis"),
			)

			handlers.RegisterListener(redisapi.Kind, manager)
			return ctx, nil
		},
	})
}
