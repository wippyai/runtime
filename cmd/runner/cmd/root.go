package cmd

import (
	"time"

	"github.com/ponyruntime/pony/cmd/internal/logger"
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
)

var rootCmd = &cobra.Command{
	Use:   "wippy",
	Short: "Smart application runtime",
	Long:  "Wippy is a smart application runtime for building and deploying distributed applications.",
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

func createLogger() (*zap.Logger, error) {
	return logger.CreateLogger(logger.Config{
		Verbose:      verbose,
		VeryVerbose:  veryVerbose,
		Console:      console,
		Silent:       silentLogs,
		AppStartTime: appStartTime,
	})
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
