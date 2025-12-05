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
	queuemgr "github.com/wippyai/runtime/service/queue/queue"
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

			if err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
				Path:        "data.driver",
				Description: "Reference to queue driver",
			}); err != nil {
				logger.Warn("failed to register driver dependency pattern", zap.Error(err))
			}

			manager := queuemgr.NewManager(
				bus,
				queueManager,
				dtt,
				logger.Named("queue.queue"),
			)

			handlers.RegisterListener("queue.queue", manager)
			return ctx, nil
		},
	})
}
