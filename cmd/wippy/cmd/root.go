package cmd

import (
	"time"

	"github.com/ponyruntime/pony/cmd/internal/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	verbose      bool
	veryVerbose  bool
	console      bool
	silentLogs   bool
	eventStreams bool
	configFile   string
	appStartTime = time.Now()
)

var rootCmd = &cobra.Command{
	Use:   "wippy",
	Short: "Dynamic runtime for AI agents and adaptive systems",
	Long: `Wippy Runtime - Live, adaptable environment for AI agents and system extensions

A dedicated runtime for deploying and managing software components that can
understand, adapt, and evolve - all within defined boundaries and governance.

Perfect for AI agent platforms, dynamic integrations, and self-modifying systems.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "config file (default is .wippy.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose debug logging")
	rootCmd.PersistentFlags().BoolVar(&veryVerbose, "very-verbose", false, "enable very verbose debug logging with stack traces")
	rootCmd.PersistentFlags().BoolVarP(&console, "console", "c", false, "enable colorful humanized console logging")
	rootCmd.PersistentFlags().BoolVarP(&silentLogs, "silent", "s", false, "disable console logging entirely")
	rootCmd.PersistentFlags().BoolVarP(&eventStreams, "event-streams", "e", false, "stream logs to event bus instead of console")
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
