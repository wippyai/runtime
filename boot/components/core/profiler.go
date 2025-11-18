package core

import (
	"context"
	"errors"
	httpbase "net/http"
	"net/http/pprof"
	"time"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"go.uber.org/zap"
)

const (
	ProfilerEnabled      boot.ConfigKey = "enabled"
	ProfilerAddress      boot.ConfigKey = "address"
	ProfilerReadTimeout  boot.ConfigKey = "read_timeout"
	ProfilerWriteTimeout boot.ConfigKey = "write_timeout"
	ProfilerIdleTimeout  boot.ConfigKey = "idle_timeout"
)

func Profiler() boot.Component {
	var server *httpbase.Server
	var logger *zap.Logger

	return boot.New(boot.P{
		Name: ProfilerName,
		Load: func(ctx context.Context) (context.Context, error) {
			logger = logapi.GetLogger(ctx)
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			cfg := boot.GetConfig(ctx)
			if cfg == nil {
				return nil
			}

			cfgSub := cfg.Sub(string(ProfilerName))
			if !cfgSub.GetBool(string(ProfilerEnabled), false) {
				return nil
			}

			addr := cfgSub.GetString(string(ProfilerAddress), "localhost:6060")
			readTimeout := cfgSub.GetDuration(string(ProfilerReadTimeout), 15*time.Second)
			writeTimeout := cfgSub.GetDuration(string(ProfilerWriteTimeout), 15*time.Second)
			idleTimeout := cfgSub.GetDuration(string(ProfilerIdleTimeout), 60*time.Second)

			mux := httpbase.NewServeMux()
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

			server = &httpbase.Server{
				Addr:         addr,
				Handler:      mux,
				ReadTimeout:  readTimeout,
				WriteTimeout: writeTimeout,
				IdleTimeout:  idleTimeout,
			}

			go func() {
				logger.Info("starting pprof server", zap.String("address", addr))
				if err := server.ListenAndServe(); err != nil {
					if !errors.Is(err, httpbase.ErrServerClosed) {
						logger.Error("pprof server failed", zap.Error(err))
					}
				}
			}()

			return nil
		},
		Stop: func(ctx context.Context) error {
			if server != nil {
				logger.Info("stopping pprof server")
				return server.Shutdown(ctx)
			}
			return nil
		},
	})
}
