package otel

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	queueapi "github.com/wippyai/runtime/api/queue"
	otelapi "github.com/wippyai/runtime/api/service/otel"
)

func OTelQueue() boot.Component {
	return boot.New(boot.P{
		Name:      OTelQueueName,
		DependsOn: []boot.Name{OTelName, queueManagerName},
		Load: func(ctx context.Context) (context.Context, error) {
			// Get OTEL service
			svc := otelapi.GetService(ctx)
			if svc == nil {
				return ctx, nil
			}

			// Get publish interceptor from service
			publishInterceptor := svc.QueuePublishInterceptor()
			if publishInterceptor == nil {
				return ctx, nil
			}

			// Get publish interceptor registry
			registry := queueapi.GetPublishInterceptorRegistry(ctx)
			if registry == nil {
				return ctx, nil
			}

			// Register the interceptor
			registry.Register("otel", publishInterceptor, 100)

			return ctx, nil
		},
	})
}
