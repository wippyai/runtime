package prometheus

import (
	"context"
	"errors"
	"net/http"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/service/prometheus"
	"go.uber.org/zap"
)

const (
	PrometheusName boot.ComponentName = "prometheus"

	metricsName boot.ComponentName = "metrics"
)

var prometheusHandlerKey = &ctxapi.Key{Name: "prometheus.handler"}

func Prometheus() boot.Component {
	var exporter *prometheus.Exporter
	var server *http.Server
	var logger *zap.Logger

	return boot.New(boot.P{
		Name:      PrometheusName,
		DependsOn: []boot.ComponentName{metricsName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger = logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, nil
			}

			bootCfg := boot.GetConfig(ctx)
			if bootCfg == nil {
				return ctx, nil
			}

			promCfg := bootCfg.Sub("prometheus")
			if promCfg == nil || !promCfg.GetBool("enabled", false) {
				logger.Debug("prometheus metrics disabled")
				return ctx, nil
			}

			collector := metricsapi.GetCollector(ctx)
			if collector == nil {
				logger.Debug("metrics collector not available")
				return ctx, nil
			}

			exporter = prometheus.NewExporter()
			if err := collector.RegisterExporter(exporter); err != nil {
				logger.Error("failed to register prometheus exporter")
				return ctx, nil
			}

			ac := ctxapi.AppFromContext(ctx)
			if ac != nil {
				ac.With(prometheusHandlerKey, exporter.Handler())
			}

			logger.Info("prometheus metrics exporter registered")
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if exporter == nil {
				if logger != nil {
					logger.Debug("prometheus exporter not initialized, skipping server start")
				}
				return nil
			}

			bootCfg := boot.GetConfig(ctx)
			if bootCfg == nil {
				logger.Debug("no boot config in Start")
				return nil
			}

			promCfg := bootCfg.Sub("prometheus")
			if promCfg == nil {
				logger.Debug("no prometheus config sub")
				return nil
			}

			addr := promCfg.GetString("address", "")
			if addr == "" {
				logger.Debug("prometheus address not configured, server disabled")
				return nil
			}

			mux := http.NewServeMux()
			mux.Handle("/metrics", exporter.Handler())

			server = &http.Server{
				Addr:    addr,
				Handler: mux,
			}

			go func() {
				logger.Info("starting prometheus metrics server", zap.String("address", addr))
				if err := server.ListenAndServe(); err != nil {
					if !errors.Is(err, http.ErrServerClosed) {
						logger.Error("prometheus server failed", zap.Error(err))
					}
				}
			}()

			return nil
		},
		Stop: func(ctx context.Context) error {
			if server != nil {
				logger.Info("stopping prometheus metrics server")
				if err := server.Shutdown(ctx); err != nil {
					logger.Error("prometheus server shutdown error", zap.Error(err))
				}
			}
			if exporter != nil {
				return exporter.Close()
			}
			return nil
		},
	})
}

func GetHandler(ctx context.Context) http.Handler {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(prometheusHandlerKey); val != nil {
		if h, ok := val.(http.Handler); ok {
			return h
		}
	}
	return nil
}

func All() []boot.Component {
	return []boot.Component{
		Prometheus(),
	}
}
