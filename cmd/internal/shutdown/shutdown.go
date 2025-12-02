// Package shutdown provides graceful shutdown functionality for wippy CLI commands.
package shutdown

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/boot"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	bootloader "github.com/wippyai/runtime/boot"
	"go.uber.org/zap"
)

// Perform executes the shutdown sequence with timeout and returns the exit code.
// It creates a fresh context with timeout for shutdown operations, executes component
// and service shutdown, and retrieves any programmatic exit code that was set.
func Perform(ctx context.Context, loader *bootloader.Loader, logger *zap.Logger, silent bool) int {
	// Get shutdown timeout from config
	cfg := boot.GetConfig(ctx)
	timeout := 30 * time.Second
	if cfg != nil {
		timeout = cfg.Sub("shutdown").GetDuration("timeout", 30*time.Second)
	}

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if !silent {
		logger.Info("shutting down")
	}

	// Shutdown components
	err := loader.Shutdown(shutdownCtx)
	if err != nil {
		logger.Error("shutdown error", zap.Error(err))
	}

	// Stop runtime services
	err = bootloader.StopRuntimeServices(shutdownCtx)
	if err != nil {
		logger.Error("failed to stop runtime services", zap.Error(err))
	}

	if !silent {
		logger.Info("stopped")
	}

	// Check for programmatic exit code
	return supervisorapi.GetExitCode(ctx)
}
