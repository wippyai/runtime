package otel

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	temporalboot "github.com/wippyai/runtime/boot/components/service/temporal"
	"github.com/wippyai/runtime/service/otel"
	"go.opentelemetry.io/otel/propagation"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
	"go.uber.org/zap"
)

// Temporal creates the OTEL tracing interceptor for Temporal workflows and activities.
// It uses the standard Temporal SDK OpenTelemetry integration for full compatibility
// with Temporal's tracing infrastructure.
func Temporal() boot.Component {
	return boot.New(boot.P{
		Name:      TemporalName,
		DependsOn: []boot.Name{Name, temporalboot.InterceptorName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}
			logger = logger.Named("otel-temporal")

			// Get boot config
			bootCfg := boot.GetConfig(ctx)
			if bootCfg == nil {
				logger.Debug("Boot config not available, skipping Temporal tracing")
				return ctx, nil
			}

			// Load OTEL config and check if Temporal tracing is enabled
			cfg := otel.LoadConfig(bootCfg)
			if !cfg.Enabled || !cfg.Temporal.Enabled {
				logger.Debug("Temporal tracing not enabled in config")
				return ctx, nil
			}

			// Get tracer from context
			tracer := otelapi.GetTracer(ctx)
			if tracer == nil {
				logger.Debug("OTEL tracer not available, skipping Temporal tracing")
				return ctx, nil
			}

			// Get interceptor registries
			clientReg := temporalapi.GetClientInterceptorRegistry(ctx)
			workerReg := temporalapi.GetWorkerInterceptorRegistry(ctx)

			if clientReg == nil || workerReg == nil {
				logger.Debug("Temporal interceptor registries not available")
				return ctx, nil
			}

			// Create Temporal tracing interceptor with the shared tracer
			tracingInterceptor, err := temporalotel.NewTracingInterceptor(temporalotel.TracerOptions{
				Tracer: tracer,
				TextMapPropagator: propagation.NewCompositeTextMapPropagator(
					propagation.TraceContext{},
					propagation.Baggage{},
				),
			})
			if err != nil {
				logger.Error("failed to create Temporal tracing interceptor", zap.Error(err))
				return ctx, nil
			}

			// Register with both client and worker registries
			// The Temporal SDK interceptor implements both interfaces
			clientReg.Register(tracingInterceptor)
			workerReg.Register(tracingInterceptor)

			logger.Info("Temporal OTEL tracing interceptor registered")

			return ctx, nil
		},
	})
}
