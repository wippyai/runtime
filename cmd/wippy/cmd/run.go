package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/boot/components/core/core"
	"github.com/ponyruntime/pony/boot/components/service/service"
	"github.com/ponyruntime/pony/boot/components/system/system"
	"github.com/ponyruntime/pony/cmd/internal/bootconfig"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the runtime with all configured components",
	Long: `Start the Wippy runtime environment

Loads and starts all configured components including core services, system
infrastructure, and application services. Components are initialized in
dependency order with proper lifecycle management.

The runtime will continue until interrupted with Ctrl+C or a termination signal.`,
	RunE: runApp,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func runApp(cmd *cobra.Command, args []string) error {
	printBanner()

	logger, err := CreateLogger()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer logger.Sync()

	logger.Info("initializing runtime")

	ctx := context.Background()
	cfg, err := loadBootConfig()
	if err != nil {
		logger.Error("failed to load config", zap.Error(err))
		return err
	}

	if cfg != nil {
		ctx = boot.WithConfig(ctx, cfg)
		logger.Info("loaded configuration")
	} else {
		ctx = boot.WithConfig(ctx, createDefaultConfig())
		logger.Info("using defaults")
	}
	components := []boot.Component{}
	components = append(components, core.All()...)
	components = append(components, system.All()...)
	components = append(components, service.All()...)

	logger.Info("registered components", zap.Int("count", len(components)))

	loader, err := bootpkg.NewLoader(components...)
	if err != nil {
		logger.Error("failed to create loader", zap.Error(err))
		return fmt.Errorf("failed to create loader: %w", err)
	}

	ctx, err = loader.Load(ctx)
	if err != nil {
		logger.Error("load failed", zap.Error(err))
		return fmt.Errorf("failed to load components: %w", err)
	}

	if err := loadEntriesFromLockFile(ctx, logger); err != nil {
		logger.Error("entry loading failed", zap.Error(err))
		return fmt.Errorf("failed to load entries: %w", err)
	}

	err = loader.Start(ctx)
	if err != nil {
		logger.Error("start failed", zap.Error(err))
		return fmt.Errorf("failed to start components: %w", err)
	}

	if !silentLogs {
		logger.Info("runtime ready")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	if !silentLogs {
		logger.Info("shutting down")
	}

	err = loader.Shutdown(ctx)
	if err != nil {
		logger.Error("shutdown error", zap.Error(err))
		return fmt.Errorf("shutdown failed: %w", err)
	}

	if !silentLogs {
		logger.Info("stopped")
	}
	return nil
}

func loadBootConfig() (boot.Config, error) {
	if configFile == "" {
		configFile = ".wippy.yaml"
	}

	cfg, err := bootconfig.Load(configFile)
	if err != nil {
		return nil, err
	}

	defaults := createDefaultConfig()
	if cfg == nil {
		return defaults, nil
	}

	return bootconfig.Merge(defaults, cfg), nil
}

func createDefaultConfig() boot.Config {
	opts := []boot.ConfigOption{}

	if verbose || veryVerbose {
		opts = append(opts, boot.WithSection("logger", map[string]interface{}{
			"mode":     "development",
			"level":    "debug",
			"encoding": "console",
		}))
	}

	if eventStreams {
		opts = append(opts, boot.WithSection("logmanager", map[string]interface{}{
			"stream_to_events": true,
		}))
	}

	return boot.NewConfig(opts...)
}
