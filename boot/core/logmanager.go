package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/system/logs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	ConfigPropagateDownstream boot.ConfigKey = "propagate_downstream"
	ConfigStreamToEvents      boot.ConfigKey = "stream_to_events"
	ConfigMinLevel            boot.ConfigKey = "min_level"
)

func LogManager() boot.Plugin {
	var logManager *logs.Manager

	return boot.New(boot.P{
		Name:      LogManagerName,
		Phase:     boot.PreInit,
		DependsOn: []string{LoggerName, EventBusName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)

			logCore := logs.NewCore(logger.Core(), bus)
			wrappedLogger := logger.WithOptions(zap.WrapCore(func(zapcore.Core) zapcore.Core {
				return logCore
			}))

			cfg := boot.GetConfig(ctx)
			var logConfig logapi.Config
			if cfg != nil {
				cfgSub := cfg.Sub(LogManagerName)
				logConfig = logapi.Config{
					PropagateDownstream: cfgSub.GetBool(string(ConfigPropagateDownstream), true),
					StreamToEvents:      cfgSub.GetBool(string(ConfigStreamToEvents), false),
					MinLevel:            zapcore.Level(cfgSub.GetInt(string(ConfigMinLevel), int(zapcore.InfoLevel))),
				}
			} else {
				logConfig = logapi.Config{
					PropagateDownstream: true,
					StreamToEvents:      false,
					MinLevel:            zapcore.InfoLevel,
				}
			}

			logManager = logs.NewManager(bus, logCore, wrappedLogger.Named("logs"), logConfig)

			return logapi.WithLogger(ctx, wrappedLogger), nil
		},
		Start: func(ctx context.Context) error {
			if logManager != nil {
				return logManager.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if logManager != nil {
				return logManager.Stop()
			}
			return nil
		},
	})
}
