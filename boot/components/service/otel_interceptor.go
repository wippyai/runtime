package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
)

func OTelInterceptor() boot.Component {
	return boot.New(boot.P{
		Name:      OTelInterceptorName,
		DependsOn: []boot.ComponentName{OTelName, bootsystem.InterceptorName},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := otelapi.GetService(ctx)
			if svc == nil {
				return ctx, nil
			}

			interceptor := svc.Interceptor()
			if interceptor == nil {
				return ctx, nil
			}

			registry := apiinterceptor.GetRegistry(ctx)
			if registry == nil {
				return ctx, nil
			}

			if err := registry.Register("otel", interceptor, 100); err != nil {
				return ctx, err
			}

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
