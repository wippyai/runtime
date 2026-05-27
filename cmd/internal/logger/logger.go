// SPDX-License-Identifier: MPL-2.0

package logger

import (
	"fmt"
	"time"

	"github.com/fatih/color"
	syslogs "github.com/wippyai/runtime/system/logs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	levelColors = map[zapcore.Level]*color.Color{
		zapcore.DebugLevel: color.New(color.FgCyan),
		zapcore.InfoLevel:  color.New(color.FgGreen),
		zapcore.WarnLevel:  color.New(color.FgYellow),
		zapcore.ErrorLevel: color.New(color.FgRed),
		zapcore.PanicLevel: color.New(color.FgMagenta),
		zapcore.FatalLevel: color.New(color.FgMagenta, color.Bold),
	}

	componentColors = []*color.Color{
		color.New(color.FgBlue),
		color.New(color.FgMagenta),
		color.New(color.FgCyan),
		color.New(color.FgYellow),
		color.New(color.FgGreen),
		color.New(color.FgRed),
		color.New(color.FgWhite),
		color.New(color.FgHiBlue),
		color.New(color.FgHiMagenta),
		color.New(color.FgHiCyan),
		color.New(color.FgHiYellow),
		color.New(color.FgHiGreen),
		color.New(color.FgHiRed),
		color.New(color.FgHiWhite),
		color.New(color.FgBlue, color.Bold),
		color.New(color.FgMagenta, color.Bold),
		color.New(color.FgCyan, color.Bold),
		color.New(color.FgYellow, color.Bold),
		color.New(color.FgGreen, color.Bold),
		color.New(color.FgRed, color.Bold),
		color.New(color.FgWhite, color.Bold),
	}
)

type Config struct {
	AppStartTime time.Time
	// Encoding selects the zap encoder. "" | "console" (default, humanized)
	// or "json" for machine-parseable structured logs. The --console CLI
	// flag and encoding from .wippy.yaml logger.encoding both feed into this.
	Encoding    string
	Verbose     bool
	VeryVerbose bool
	Console     bool
	Silent      bool
}

func consoleTimeEncoder(startTime time.Time) zapcore.TimeEncoder {
	return func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		elapsed := t.Sub(startTime)
		enc.AppendString(fmt.Sprintf("%6.2fs", elapsed.Seconds()))
	}
}

func consoleLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	if levelColor, ok := levelColors[level]; ok {
		enc.AppendString(levelColor.Sprintf("%-5s", level.CapitalString()))
	} else {
		enc.AppendString(fmt.Sprintf("%-5s", level.CapitalString()))
	}
}

func consoleNameEncoder(loggerName string, enc zapcore.PrimitiveArrayEncoder) {
	if loggerName == "" {
		enc.AppendString("")
		return
	}

	hash := 0
	for _, r := range loggerName {
		hash = hash*31 + int(r)
	}
	colorIndex := hash % len(componentColors)
	if colorIndex < 0 {
		colorIndex = -colorIndex
	}

	componentColor := componentColors[colorIndex]
	enc.AppendString(componentColor.Sprintf("%-12s", loggerName))
}

func CreateLogger(cfg Config) (*zap.Logger, error) {
	if cfg.Silent {
		return zap.NewNop(), nil
	}

	var zapCfg zap.Config

	// Console flag takes precedence (legacy -c behavior); otherwise
	// honor Encoding from config. Empty Encoding falls back to
	// development console output for readability.
	switch {
	case cfg.Console:
		zapCfg = zap.Config{
			Level:       zap.NewAtomicLevelAt(zapcore.InfoLevel),
			Development: true,
			Encoding:    "console",
			EncoderConfig: zapcore.EncoderConfig{
				TimeKey:       "time",
				LevelKey:      "level",
				NameKey:       "name",
				CallerKey:     "",
				MessageKey:    "msg",
				StacktraceKey: "",
				LineEnding:    zapcore.DefaultLineEnding,
				EncodeTime:    consoleTimeEncoder(cfg.AppStartTime),
				EncodeLevel:   consoleLevelEncoder,
				EncodeName:    consoleNameEncoder,
				EncodeCaller:  nil,
			},
			OutputPaths:      []string{"stdout"},
			ErrorOutputPaths: []string{"stderr"},
		}
	case cfg.Encoding == "json":
		zapCfg = zap.NewProductionConfig()
		zapCfg.EncoderConfig.TimeKey = "ts"
		zapCfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		zapCfg.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
		zapCfg.EncoderConfig.EncodeCaller = nil
		zapCfg.EncoderConfig.CallerKey = ""
		zapCfg.DisableCaller = true
		zapCfg.DisableStacktrace = true
		zapCfg.OutputPaths = []string{"stdout"}
		zapCfg.ErrorOutputPaths = []string{"stderr"}
	default:
		zapCfg = zap.NewDevelopmentConfig()
		zapCfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)
		zapCfg.DisableCaller = true
	}

	switch {
	case cfg.VeryVerbose:
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		if !cfg.Console {
			zapCfg.DisableStacktrace = false
		}
	case cfg.Verbose:
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		zapCfg.DisableStacktrace = true
	default:
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
		zapCfg.DisableStacktrace = true
		// Defense-in-depth log sampling for the steady-state path. Console
		// and Verbose modes intentionally skip this so debugging stays
		// honest. Under chaos partition we observed ~thousands of identical
		// WARN/ERROR lines per second; sampling caps that at ~100/s for
		// the first second of any unique message, then 1-in-100 thereafter.
		// The per-site fixes (metric counters) remain primary; this is
		// purely a fail-safe so a future hot WARN cannot OOM the runtime.
		if !cfg.Console {
			zapCfg.Sampling = &zap.SamplingConfig{
				Initial:    100,
				Thereafter: 100,
			}
		}
	}

	// zap.Hooks fires the closure on every emission. The hook itself
	// is a no-op until the metrics boot component calls
	// syslogs.SetEmissionCollector — see system/logs/emissionhook.go.
	logger, err := zapCfg.Build(zap.Hooks(syslogs.EmissionHook))
	if err != nil {
		return nil, NewBuildLoggerError(err)
	}

	return logger, nil
}
