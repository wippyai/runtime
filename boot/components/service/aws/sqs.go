// SPDX-License-Identifier: MPL-2.0

package aws

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	sqsapi "github.com/wippyai/runtime/api/service/aws/sqs"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	bootqueue "github.com/wippyai/runtime/boot/components/queue"
	"github.com/wippyai/runtime/service/aws/sqs"
)

func SQS() boot.Component {
	return boot.New(boot.P{
		Name:      SQSName,
		DependsOn: []boot.Name{bootcore.RegistryName, bootqueue.ManagerName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			dtt := payload.GetTranscoder(ctx)
			if dtt == nil {
				return ctx, ErrTranscoderNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, ErrHandlerRegistryNotAvailable
			}

			manager := sqs.NewManager(
				bus,
				dtt,
				logger.Named("queue.sqs"),
			)

			handlers.RegisterListener(sqsapi.Kind, manager)
			return ctx, nil
		},
	})
}
