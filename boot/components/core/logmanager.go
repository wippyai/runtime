package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/system/logs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	ConfigPropagateDownstream boot.ConfigKey = "propagate_downstream"
	ConfigStreamToEvents      boot.ConfigKey = "stream_to_events"
	ConfigMinLevel            boot.ConfigKey = "min_level"
)

func LogManager() boot.Component {
	var logManager *logs.Manager
	var wrappedLogger *zap.Logger

	return boot.New(boot.P{
		Name:      LogManagerName,
		DependsOn: []boot.ComponentName{EventBusName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)

			logCore := logs.NewCore(logger.Core(), bus)
			wrappedLogger = logger.WithOptions(zap.WrapCore(func(zapcore.Core) zapcore.Core {
				return logCore
			}))

			cfg := boot.GetConfig(ctx)
			var logConfig logapi.Config
			if cfg != nil {
				cfgSub := cfg.Sub(string(LogManagerName))
				logConfig = logapi.Config{
					PropagateDownstream: cfgSub.GetBool(string(ConfigPropagateDownstream), true),
					StreamToEvents:      cfgSub.GetBool(string(ConfigStreamToEvents), false),
					MinLevel:            zapcore.Level(cfgSub.GetInt(string(ConfigMinLevel), int(zapcore.InfoLevel))), //nolint:gosec // int to zapcore.Level conversion
				}
			} else {
				logConfig = logapi.Config{
					PropagateDownstream: true,
					StreamToEvents:      false,
					MinLevel:            zapcore.InfoLevel,
				}
			}

			logManager = logs.NewManager(bus, logCore, wrappedLogger.Named("logs"), logConfig)

			// Update the logger in context with the wrapped version
			return logapi.UpdateLogger(ctx, wrappedLogger), nil
		},
		Start: func(ctx context.Context) error {
			if logManager != nil {
				return logManager.Start(ctx)
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if logManager != nil {
				return logManager.Stop()
			}
			return nil
		},
	})
}
