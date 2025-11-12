//go:build !plugin_minimal

package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	otelapi "github.com/ponyruntime/pony/api/service/otel"
	otelinterceptor "github.com/ponyruntime/pony/service/interceptor/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func InterceptorOtel() boot.Plugin {
	return boot.New(boot.P{
		Name:      InterceptorOtelName,
		Phase:     boot.PostInit,
		DependsOn: []string{"eventbus", "interceptor-manager"},
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
