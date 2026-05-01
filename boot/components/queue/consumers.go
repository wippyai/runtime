// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/queue/consumer"
)

func Consumers() boot.Component {
	return boot.New(boot.P{
		Name: ConsumersName,
		DependsOn: []boot.Name{
			bootcore.RegistryName,
			ManagerName,
			bootsystem.FunctionsName,
		},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			queueMgr := queueapi.GetManager(ctx)
			funcReg := function.GetRegistry(ctx)

			manager := consumer.NewManager(
				bus,
				queueMgr,
				funcReg,
				dtt,
				logger.Named("queue.consumer"),
			)

			handlers.RegisterListener("queue.consumer", manager)
			return ctx, nil
		},
	})
}
