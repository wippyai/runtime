package otel

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/service/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

func Metrics() boot.Component {
	var mp metric.MeterProvider
	var exporter *otel.MetricsExporter

	return boot.New(boot.P{
		Name:      MetricsName,
		DependsOn: []boot.Name{Name, metricsName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("otel.metrics")
			if logger == nil {
				return ctx, nil
			}

			bootCfg := boot.GetConfig(ctx)
			if bootCfg == nil {
				return ctx, nil
			}

			cfg := otel.LoadConfig(bootCfg)
			if !cfg.Enabled || !cfg.MetricsEnabled {
				logger.Debug("OTEL metrics disabled")
				return ctx, nil
			}

			collector := metricsapi.GetCollector(ctx)
			if collector == nil {
				logger.Debug("metrics collector not available")
				return ctx, nil
			}

			var err error
			mp, err = otel.InitializeMeterProvider(ctx, cfg, logger)
			if err != nil {
				logger.Error("failed to initialize OTEL meter provider", zap.Error(err))
				return ctx, nil
			}

			exporter = otel.NewMetricsExporter(mp)
			if err := collector.RegisterExporter(exporter); err != nil {
				logger.Error("failed to register OTEL metrics exporter", zap.Error(err))
				return ctx, nil
			}

			logger.Info("OTEL metrics exporter registered")
			return ctx, nil
		},
		Stop: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			if mp != nil && logger != nil {
				return otel.ShutdownMeterProvider(ctx, mp, logger)
			}
			return nil
		},
	})
}
