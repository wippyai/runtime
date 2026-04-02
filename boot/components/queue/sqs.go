// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	sqsapi "github.com/wippyai/runtime/api/service/queue/sqs"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/queue/sqs"
)

func SQS() boot.Component {
	return boot.New(boot.P{
		Name:      SQSDriverName,
		DependsOn: []boot.Name{ManagerName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

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
