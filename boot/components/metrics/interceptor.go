package metrics

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	apifunction "github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	api "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/system/metrics/interceptor"
	"go.uber.org/zap"
)

func MetricsInterceptor() boot.Component {
	return boot.New(boot.P{
		Name:      MetricsInterceptorName,
		DependsOn: []boot.ComponentName{MetricsName, interceptorName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("metrics.interceptor")

			bootCfg := boot.GetConfig(ctx)
			if bootCfg == nil {
				return ctx, nil
			}

			metricsCfg := bootCfg.Sub("metrics")
			if metricsCfg == nil {
				return ctx, nil
			}

			enabled := metricsCfg.GetBool("interceptor.enabled", false)
			if !enabled {
				if logger != nil {
					logger.Debug("metrics interceptor disabled")
				}
				return ctx, nil
			}

			collector := api.GetCollector(ctx)
			if collector == nil {
				if logger != nil {
					logger.Debug("metrics collector not available for interceptor")
				}
				return ctx, nil
			}

			registry := apifunction.GetInterceptorRegistry(ctx)
			if registry == nil {
				if logger != nil {
					logger.Debug("function interceptor registry not available")
				}
				return ctx, nil
			}

			metricsInterceptor := interceptor.NewFunctionInterceptor(collector, true)
			if err := registry.Register("metrics", metricsInterceptor, 50); err != nil {
				return ctx, err
			}

			if logger != nil {
				logger.Debug("interceptor registered", zap.String("interceptor", "metrics"), zap.Int("order", 50))
			}

			return ctx, nil
		},
	})
}
