package cmd

import (
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	"go.uber.org/zap"
)

// createCommandLogger is the single logger-construction path for command
// execution flows. It keeps CLI flags -> logger config mapping consistent.
func createCommandLogger() (*zap.Logger, error) {
	return clilogger.CreateLogger(clilogger.Config{
		Verbose:      verbose,
		VeryVerbose:  veryVerbose,
		Console:      console,
		Silent:       silentLogs,
		AppStartTime: appStartTime,
	})
}
