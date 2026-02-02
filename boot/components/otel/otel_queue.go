package otel

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	queueapi "github.com/wippyai/runtime/api/queue"
	otelapi "github.com/wippyai/runtime/api/service/otel"
)

func Queue() boot.Component {
	return boot.New(boot.P{
		Name:      QueueName,
		DependsOn: []boot.Name{Name, queueManagerName},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := otelapi.GetService(ctx)
			if svc == nil {
				return ctx, nil
			}

			publishInterceptor := svc.QueuePublishInterceptor()
			if publishInterceptor == nil {
				return ctx, nil
			}

			mgr := queueapi.GetManager(ctx)
			if mgr == nil {
				return ctx, nil
			}

			mgr.RegisterInterceptor("otel", publishInterceptor, 100)

			return ctx, nil
		},
	})
}
