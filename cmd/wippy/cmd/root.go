package cmd

import (
	"time"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/cmd/internal/logger"
	"go.uber.org/zap"
)

var (
	verbose      bool
	veryVerbose  bool
	console      bool
	silentLogs   bool
	eventStreams bool
	profiler     bool
	configFile   string
	appStartTime = time.Now()
)

var rootCmd = &cobra.Command{
	Use:           "wippy",
	Short:         "Adaptive Application Runtime",
	Long:          `Run applications with dynamic configuration and lifecycle management.`,
	SilenceErrors: true,
	SilenceUsage:  true,
	Run: func(cmd *cobra.Command, args []string) {
		printBanner()
		_ = cmd.Help() // Ignore help output errors
	},
}

func Execute() error {
	return rootCmd.Execute()
}

// IsConsoleMode returns whether console mode is enabled
func IsConsoleMode() bool {
	return console
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "config file (default is .wippy.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose debug logging")
	rootCmd.PersistentFlags().BoolVar(&veryVerbose, "very-verbose", false, "enable very verbose debug logging with stack traces")
	rootCmd.PersistentFlags().BoolVarP(&console, "console", "c", false, "enable colorful humanized console logging")
	rootCmd.PersistentFlags().BoolVarP(&silentLogs, "silent", "s", false, "disable console logging entirely")
	rootCmd.PersistentFlags().BoolVarP(&eventStreams, "event-streams", "e", false, "stream logs to event bus instead of console")
	rootCmd.PersistentFlags().BoolVarP(&profiler, "profiler", "p", false, "enable pprof profiler on localhost:6060")
}

func CreateLogger() (*zap.Logger, error) {
	return logger.CreateLogger(logger.Config{
		Verbose:      verbose,
		VeryVerbose:  veryVerbose,
		Console:      console,
		Silent:       silentLogs,
		AppStartTime: appStartTime,
	})
}
