package otel

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	"github.com/wippyai/runtime/service/otel"
	"go.uber.org/zap"
)

func OTel() boot.Component {
	return boot.New(boot.P{
		Name: OTelName,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("otel")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bootCfg := boot.GetConfig(ctx)
			if bootCfg == nil {
				return ctx, ErrBootConfigNotAvailable
			}

			cfg := otel.LoadConfig(bootCfg)
			otel.ApplyEnvOverrides(&cfg, logger)
			otel.LogConfigSources(cfg, logger)

			if !cfg.Enabled {
				logger.Debug("OTEL disabled")
				return ctx, nil
			}

			tp, err := otel.InitializeProvider(ctx, cfg, logger)
			if err != nil {
				return ctx, NewOTELInitError(err)
			}

			tracer := tp.Tracer("wippy-runtime")
			ctx = otelapi.WithTracer(ctx, tracer)

			svc := otel.NewService(cfg, logger, tp)
			ctx = otelapi.WithService(ctx, svc)

			logger.Info("OTEL service initialized", zap.Bool("enabled", cfg.Enabled))

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
