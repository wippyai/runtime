package otel

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	apiinterceptor "github.com/wippyai/runtime/api/function"
	otelapi "github.com/wippyai/runtime/api/service/otel"
)

func OTelInterceptor() boot.Component {
	return boot.New(boot.P{
		Name:      OTelInterceptorName,
		DependsOn: []boot.Name{OTelName, interceptorName},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := otelapi.GetService(ctx)
			if svc == nil {
				return ctx, nil
			}

			interceptor := svc.Interceptor()
			if interceptor == nil {
				return ctx, nil
			}

			registry := apiinterceptor.GetInterceptorRegistry(ctx)
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
