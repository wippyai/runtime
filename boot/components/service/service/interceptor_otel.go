package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	bootsystem "github.com/wippyai/runtime/boot/components/system/system"
	otelinterceptor "github.com/wippyai/runtime/service/interceptor/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func InterceptorOtel() boot.Component {
	return boot.New(boot.P{
		Name:      InterceptorOtelName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{bootsystem.InterceptorName},
		Load: func(ctx context.Context) (context.Context, error) {
			tracerProvider := otel.GetTracerProvider()
			if tracerProvider == nil || tracerProvider == noop.NewTracerProvider() {
				return ctx, nil
			}

			tracer := tracerProvider.Tracer("pony-runtime")
			ctx = otelapi.WithTracer(ctx, tracer)

			registry := apiinterceptor.GetRegistry(ctx)
			if registry == nil {
				return ctx, nil
			}

			if err := registry.Register("otel", otelinterceptor.New(), 100); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
