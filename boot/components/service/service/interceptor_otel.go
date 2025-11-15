package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
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
			bus := event.GetBus(ctx)

			tracerProvider := otel.GetTracerProvider()
			if tracerProvider == nil || tracerProvider == noop.NewTracerProvider() {
				return ctx, nil
			}

			tracer := tracerProvider.Tracer("pony-runtime")
			ctx = otelapi.WithTracer(ctx, tracer)

			bus.Send(ctx, event.Event{
				System: "interceptor",
				Kind:   "interceptor.register",
				Path:   "interceptor/otel",
				Data:   payload.New(otelinterceptor.New()),
			})

			return ctx, nil
		},
	})
}
