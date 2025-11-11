package cmd

import (
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	verbose      bool
	veryVerbose  bool
	console      bool
	silentLogs   bool
	eventStreams bool

	appStartTime = time.Now()

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

var rootCmd = &cobra.Command{
	Use:           "wippy",
	Short:         "Smart application runtime",
	Long:          "Wippy is a smart application runtime for building and deploying distributed applications.",
	SilenceUsage:  true,
	SilenceErrors: false,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose debug logging")
	rootCmd.PersistentFlags().BoolVar(&veryVerbose, "very-verbose", false, "enable very verbose debug logging with stack traces")
	rootCmd.PersistentFlags().BoolVarP(&console, "console", "c", false, "enable colorful humanized console logging")
	rootCmd.PersistentFlags().BoolVarP(&silentLogs, "silent", "s", false, "disable console logging entirely")
	rootCmd.PersistentFlags().BoolVarP(&eventStreams, "event-streams", "e", false, "enable event streaming to capture all logs")
}

func consoleTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	elapsed := t.Sub(appStartTime)
	enc.AppendString(fmt.Sprintf("%6.2fs", elapsed.Seconds()))
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
	colorIndex := (hash % len(componentColors))
	if colorIndex < 0 {
		colorIndex = -colorIndex
	}

	componentColor := componentColors[colorIndex]
	enc.AppendString(componentColor.Sprintf("%-12s", loggerName))
}

func createLogger() (*zap.Logger, error) {
	if silentLogs {
		return zap.NewNop(), nil
	}

	var cfg zap.Config

	if console {
		cfg = zap.Config{
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
				EncodeTime:    consoleTimeEncoder,
				EncodeLevel:   consoleLevelEncoder,
				EncodeName:    consoleNameEncoder,
				EncodeCaller:  nil,
			},
			OutputPaths:      []string{"stdout"},
			ErrorOutputPaths: []string{"stderr"},
		}
	} else {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)
		cfg.DisableCaller = true
	}

	switch {
	case veryVerbose:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		if !console {
			cfg.DisableStacktrace = false
		}
	case verbose:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		cfg.DisableStacktrace = true
	default:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
		cfg.DisableStacktrace = true
	}

	logger, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build logger: %w", err)
	}

	return logger, nil
}

func GetLoggingConfig() (consoleEnabled bool, eventEnabled bool) {
	consoleEnabled = !silentLogs
	eventEnabled = eventStreams
	return consoleEnabled, eventEnabled
}

func GetVerboseLevel() zapcore.Level {
	switch {
	case veryVerbose, verbose:
		return zapcore.DebugLevel
	default:
		return zapcore.InfoLevel
	}
}
