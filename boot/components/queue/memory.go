package queue

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	memoryapi "github.com/wippyai/runtime/api/service/queue/memory"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/queue/memory"
)

func Memory() boot.Component {
	return boot.New(boot.P{
		Name:      MemoryDriverName,
		DependsOn: []boot.ComponentName{ManagerName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := memory.NewManager(
				bus,
				dtt,
				logger.Named("queue.memory"),
			)

			handlers.RegisterListener(memoryapi.Kind, manager)
			return ctx, nil
		},
	})
}
