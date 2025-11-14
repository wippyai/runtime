package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/payload"
	regapi "github.com/ponyruntime/pony/api/registry"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/boot/build"
	"github.com/ponyruntime/pony/boot/build/stages"
	"github.com/ponyruntime/pony/boot/components/core/core"
	"github.com/ponyruntime/pony/boot/components/service/service"
	"github.com/ponyruntime/pony/boot/components/system/system"
	transcoder "github.com/ponyruntime/pony/system/payload"
	json2 "github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	regtop "github.com/ponyruntime/pony/system/registry/topology"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var runDumpCmd = &cobra.Command{
	Use:   "run-dump",
	Short: "Run application from state dump",
	Long: `Load application state from JSON dump file and run the application

Loads the pre-link state from a dump file, executes all pipeline stages
(override, disable, link), and then starts the runtime with all configured components.`,
	RunE: runAppFromDump,
}

func init() {
	rootCmd.AddCommand(runDumpCmd)

	runDumpCmd.Flags().StringP("dump", "d", "state.json", "path to state dump file")
}

func runAppFromDump(cmd *cobra.Command, args []string) error {
	printBanner()

	logger, err := CreateLogger()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer logger.Sync()

	dumpFile, _ := cmd.Flags().GetString("dump")

	logger.Info("initializing runtime from dump", zap.String("dump", dumpFile))

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

	// todO: this is wrong
	dtt := transcoder.GlobalTranscoder()
	json2.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	ctx = payload.WithTranscoder(ctx, dtt)

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

	logger.Info("loading entries from dump (no pipeline stages)")

	entries := []regapi.Entry{}

	pipeline := build.New(
		stages.LoadDump(dumpFile),
	)

	if err := pipeline.Execute(ctx, &entries); err != nil {
		logger.Error("loading dump failed", zap.Error(err))
		return fmt.Errorf("failed to load dump: %w", err)
	}

	logger.Info("pipeline executed", zap.Int("entries", len(entries)))

	changeSet, err := regtop.NewStateBuilder(logger.Named("state-builder")).BuildDelta(regapi.State{}, entries)
	if err != nil {
		logger.Error("failed to build change set", zap.Error(err))
		return fmt.Errorf("build change set: %w", err)
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		return fmt.Errorf("registry not found in context")
	}

	version, err := reg.Apply(ctx, changeSet)
	if err != nil {
		logger.Error("failed to apply change set", zap.Error(err))
		return fmt.Errorf("apply change set: %w", err)
	}

	logger.Debug("registry updated", zap.Any("version", version))

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
