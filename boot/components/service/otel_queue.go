package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	queueapi "github.com/wippyai/runtime/api/queue"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	bootqueue "github.com/wippyai/runtime/boot/components/queue"
)

func OTelQueue() boot.Component {
	return boot.New(boot.P{
		Name:      OTelQueueName,
		DependsOn: []boot.ComponentName{OTelName, bootqueue.ManagerName},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := otelapi.GetService(ctx)
			if svc == nil {
				return ctx, nil
			}

			interceptor := svc.QueuePublishInterceptor()
			if interceptor == nil {
				return ctx, nil
			}

			registry := queueapi.GetPublishInterceptorRegistry(ctx)
			if registry == nil {
				return ctx, nil
			}

			registry.Register("otel", interceptor, 100)

			return ctx, nil
		},
		Start: func(_ context.Context) error {
			return nil
		},
		Stop: func(_ context.Context) error {
			return nil
		},
	})
}
