// SPDX-License-Identifier: MPL-2.0

package prometheus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/service/metrics/prometheus"
	"github.com/wippyai/runtime/system/health"
	"go.uber.org/zap"
)

const (
	Name boot.Name = "prometheus"

	metricsName boot.Name = "metrics"
)

var prometheusHandlerKey = &ctxapi.Key{Name: "prometheus.handler"}

func Prometheus() boot.Component {
	var exporter *prometheus.Exporter
	var server *http.Server
	var logger *zap.Logger

	return boot.New(boot.P{
		Name:      Name,
		DependsOn: []boot.Name{metricsName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger = logapi.GetLogger(ctx).Named("prometheus")
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
			mux.HandleFunc("/livez", livezHandler(logger))

			// pprof handlers are gated by WIPPY_DEBUG_PPROF=1 so they are
			// off by default in production. When enabled, they share the
			// metrics listener so a single port-forward exposes both
			// /metrics and /debug/pprof/*. Useful for capturing heap/allocs
			// during chaos without bumping the memory limit to mask growth.
			if os.Getenv("WIPPY_DEBUG_PPROF") == "1" {
				mux.HandleFunc("/debug/pprof/", pprof.Index)
				mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
				mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
				mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
				mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
				logger.Info("pprof handlers enabled at /debug/pprof/*")
			}

			server = &http.Server{
				Addr:              addr,
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
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

func All() []boot.Component {
	return []boot.Component{
		Prometheus(),
	}
}

// livezHandler returns 200 only when every registered liveness check
// reports healthy. The body lists each check + status so kubectl
// describe / kubelet event logs make the failing check visible.
//
// This is the activity-based liveness probe that replaces the prior
// TCP-only check. Without it a pod can stay Ready while stuck on the
// minority side of a network partition — observed in the original
// chaos run as one pod idle at 49 MiB while peers held 474 MiB.
func livezHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		results := health.Run()
		var failed []string
		var body strings.Builder
		for _, res := range results {
			status := "ok"
			if res.Err != nil {
				status = "fail: " + res.Err.Error()
				failed = append(failed, res.Name)
			}
			fmt.Fprintf(&body, "%s\t%s\n", res.Name, status)
		}
		if len(results) == 0 {
			body.WriteString("(no liveness checks registered)\n")
		}

		w.Header().Set("Content-Type", "text/plain")
		if len(failed) > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			if logger != nil {
				logger.Warn("/livez 503", zap.Strings("failed", failed))
			}
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_, _ = w.Write([]byte(body.String()))
	}
}
