package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/pidgen"
	"github.com/ponyruntime/pony/internal/uniqid"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func Logger() boot.Plugin {
	return boot.New(boot.P{
		Name:  LoggerName,
		Phase: boot.PreInit,
		Load: func(ctx context.Context) (context.Context, error) {
			var logger *zap.Logger
			var err error

			cfg := boot.GetConfig(ctx)
			if cfg != nil {
				logCfg := cfg.Sub(LoggerName)
				mode := logCfg.GetString(string(LoggerMode), "production")
				levelStr := logCfg.GetString(string(LoggerLevel), "info")
				encoding := logCfg.GetString(string(LoggerEncoding), "json")

				var level zapcore.Level
				if err := level.UnmarshalText([]byte(levelStr)); err != nil {
					level = zapcore.InfoLevel
				}

				zapConfig := zap.Config{
					Level:            zap.NewAtomicLevelAt(level),
					Encoding:         encoding,
					EncoderConfig:    zap.NewProductionEncoderConfig(),
					OutputPaths:      []string{"stdout"},
					ErrorOutputPaths: []string{"stderr"},
				}

				if mode == "development" {
					zapConfig.Development = true
					zapConfig.EncoderConfig = zap.NewDevelopmentEncoderConfig()
				}

				logger, err = zapConfig.Build()
			} else {
				logger, err = zap.NewProduction()
			}

			if err != nil {
				return ctx, err
			}
			return logapi.WithLogger(ctx, logger), nil
		},
	})
}

func EventBus() boot.Plugin {
	return boot.New(boot.P{
		Name:  EventBusName,
		Phase: boot.PreInit,
		Load: func(ctx context.Context) (context.Context, error) {
			bus := eventbus.NewBus()
			return event.WithBus(ctx, bus), nil
		},
	})
}

func PIDGen() boot.Plugin {
	return boot.New(boot.P{
		Name:  PIDGenName,
		Phase: boot.PreInit,
		Load: func(ctx context.Context) (context.Context, error) {
			uniqGen := uniqid.NewGenerator()
			gen := uniqid.NewPIDGenerator(uniqGen, "local")
			return pidgen.WithGenerator(ctx, gen), nil
		},
	})
}
