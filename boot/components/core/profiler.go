package core

import (
	"context"
	"errors"
	"fmt"
	httpbase "net/http"
	"net/http/pprof"
	"runtime"
	"time"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/wasm-runtime/engine"
	"go.uber.org/zap"
)

const (
	ProfilerEnabled      boot.Name = "enabled"
	ProfilerAddress      boot.Name = "address"
	ProfilerReadTimeout  boot.Name = "read_timeout"
	ProfilerWriteTimeout boot.Name = "write_timeout"
	ProfilerIdleTimeout  boot.Name = "idle_timeout"
)

func Profiler() boot.Component {
	var server *httpbase.Server
	var logger *zap.Logger

	return boot.New(boot.P{
		Name: ProfilerName,
		Load: func(ctx context.Context) (context.Context, error) {
			logger = logapi.GetLogger(ctx).Named("pprof")
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
			mux.HandleFunc("/debug/gc", func(w httpbase.ResponseWriter, r *httpbase.Request) {
				runtime.GC()
				runtime.GC()
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				fmt.Fprintf(w, "GC done. HeapAlloc=%dMB HeapObjects=%d\n",
					m.HeapAlloc/1024/1024, m.HeapObjects)
			})
			mux.HandleFunc("/debug/stats", func(w httpbase.ResponseWriter, r *httpbase.Request) {
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				fmt.Fprintf(w, "HeapAlloc=%dMB HeapObjects=%d\n",
					m.HeapAlloc/1024/1024, m.HeapObjects)
			})
			mux.HandleFunc("/debug/wazevo", func(w httpbase.ResponseWriter, r *httpbase.Request) {
				compiles, deletes, cacheHits, mapSize := engine.GetWazevoStats()
				linkerCompiled, linkerClosed, linkersCreated, linkersClosed := engine.GetLinkerStats()
				fmt.Fprintf(w, "wazevo_compiles=%d\n", compiles)
				fmt.Fprintf(w, "wazevo_deletes=%d\n", deletes)
				fmt.Fprintf(w, "wazevo_cache_hits=%d\n", cacheHits)
				fmt.Fprintf(w, "wazevo_map_size=%d\n", mapSize)
				fmt.Fprintf(w, "linker_modules_compiled=%d\n", linkerCompiled)
				fmt.Fprintf(w, "linker_modules_closed=%d\n", linkerClosed)
				fmt.Fprintf(w, "linkers_created=%d\n", linkersCreated)
				fmt.Fprintf(w, "linkers_closed=%d\n", linkersClosed)
			})

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
