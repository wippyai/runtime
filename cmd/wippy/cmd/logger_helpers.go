// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	"go.uber.org/zap"
)

// createCommandLogger is the single logger-construction path for command
// execution flows. It keeps CLI flags -> logger config mapping consistent.
//
// Logger encoding resolution (highest precedence first):
//  1. --console / -c flag (humanized console)
//  2. logger.encoding from .wippy.yaml (canonical config)
//  3. development console default
func createCommandLogger() (*zap.Logger, error) {
	return clilogger.CreateLogger(clilogger.Config{
		Verbose:      verbose,
		VeryVerbose:  veryVerbose,
		Console:      console,
		Silent:       silentLogs,
		AppStartTime: appStartTime,
		Encoding:     preloadLoggerEncoding(),
	})
}

// preloadLoggerEncoding peeks the config file (if present) for the
// logger.encoding key so the CLI bootstrap logger can emit structured
// JSON from the first line. Returns "" on any failure — the caller
// falls back to the humanized development encoder.
func preloadLoggerEncoding() string {
	path := configFile
	if path == "" {
		path = defaultConfigFile
	}
	cfg, err := bootconfig.Load(path)
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.GetString("logger.encoding", "")
}
