package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/pack"
	regtop "github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

var runPackCmd = &cobra.Command{
	Use:   "run-pack <pack1.wapp> [pack2.wapp...]",
	Short: "Start the runtime from pack files",
	Long: `Start the Wippy runtime environment from one or more pack files

Pack files contain pre-linked entries and are loaded directly without
running any pipeline stages. Multiple packs are loaded in order and
merged before applying to the registry.

Examples:
  wippy run-pack snapshot.wapp
  wippy run-pack base.wapp overlay.wapp`,
	Args: cobra.MinimumNArgs(1),
	RunE: runFromPack,
}

func init() {
	rootCmd.AddCommand(runPackCmd)
}

func runFromPack(cmd *cobra.Command, args []string) error {
	printBanner()

	logger, err := CreateLogger()
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer logger.Sync()

	logger.Info("loading pack files", zap.Int("count", len(args)))

	cfg, err := loadBootConfig()
	if err != nil {
		logger.Error("failed to load config", zap.Error(err))
		return err
	}

	if cfg == nil {
		cfg = createDefaultConfig()
	}

	ctx, err := bootpkg.NewBootstrapContext(logger, cfg)
	if err != nil {
		logger.Error("failed to initialize bootstrap context", zap.Error(err))
		return fmt.Errorf("initialize bootstrap context: %w", err)
	}

	logger = logapi.GetLogger(ctx).Named("run-pack")
	logger.Info("infrastructure initialized")

	components := StandardComponents()
	logger.Info("registered components", zap.Int("count", len(components)))

	loader, err := bootpkg.NewLoader(components...)
	if err != nil {
		logger.Error("failed to create loader", zap.Error(err))
		return fmt.Errorf("create loader: %w", err)
	}

	ctx, err = loader.Load(ctx)
	if err != nil {
		logger.Error("load failed", zap.Error(err))
		return fmt.Errorf("load components: %w", err)
	}
	logger.Info("components loaded successfully")

	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return fmt.Errorf("transcoder not found")
	}

	packer := pack.New(transcoder)
	var allEntries []regapi.Entry

	for _, packFile := range args {
		logger.Info("unpacking", zap.String("file", packFile))

		file, err := os.Open(packFile)
		if err != nil {
			return fmt.Errorf("open pack file %s: %w", packFile, err)
		}

		entries, metadata, err := packer.Unpack(file)
		if closeErr := file.Close(); closeErr != nil {
			logger.Warn("error closing pack file", zap.String("file", packFile), zap.Error(closeErr))
		}
		if err != nil {
			return fmt.Errorf("unpack %s: %w", packFile, err)
		}

		if metadata != nil {
			logger.Info("pack metadata",
				zap.String("file", packFile),
				zap.String("wippy_version", metadata.GetString("wippy_version", "")),
				zap.String("commit", metadata.GetString("wippy_commit", "")),
				zap.String("packed_at", metadata.GetString("packed_at", "")),
				zap.Int("entry_count", metadata.GetInt("entry_count", 0)),
				zap.String("description", metadata.GetString("description", "")))
		}

		logger.Info("unpacked entries",
			zap.String("file", packFile),
			zap.Int("count", len(entries)))

		allEntries = append(allEntries, entries...)
	}

	logger.Info("total entries loaded", zap.Int("count", len(allEntries)))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		sig := <-sigChan
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))
		cancel()

		sig = <-sigChan
		logger.Warn("received second shutdown signal, forcing exit", zap.String("signal", sig.String()))
		os.Exit(1)
	}()

	err = bootpkg.StartRuntimeServices(appCtx)
	if err != nil {
		logger.Error("failed to start runtime services", zap.Error(err))
		return fmt.Errorf("start runtime services: %w", err)
	}

	err = loader.Start(appCtx)
	if err != nil {
		logger.Error("start failed", zap.Error(err))
		return fmt.Errorf("start components: %w", err)
	}

	reg := regapi.GetRegistry(appCtx)
	if reg == nil {
		return fmt.Errorf("registry not found")
	}

	resolver := regapi.GetResolver(appCtx)
	if resolver == nil {
		return fmt.Errorf("dependency resolver not found")
	}

	logger.Debug("building change set from entries")
	// Use CreateChangeSetFromEntries which properly sorts by dependencies
	changeSet, err := regtop.CreateChangeSetFromEntries(allEntries, resolver)
	if err != nil {
		return fmt.Errorf("build change set: %w", err)
	}
	logger.Debug("change set built")

	logger.Info("applying change set to registry", zap.Int("entry_count", len(allEntries)))
	version, err := reg.Apply(appCtx, changeSet)
	if err != nil {
		logger.Error("apply failed", zap.Error(err))
		return fmt.Errorf("apply entries: %w", err)
	}

	logger.Info("entries applied to registry", zap.Any("version", version))

	if !silentLogs {
		logger.Info("runtime ready")
	}

	<-appCtx.Done()
	if !silentLogs {
		logger.Info("shutting down")
	}

	err = loader.Shutdown(ctx)
	if err != nil {
		logger.Error("shutdown error", zap.Error(err))
		return fmt.Errorf("shutdown failed: %w", err)
	}

	err = bootpkg.StopRuntimeServices(ctx)
	if err != nil {
		logger.Error("failed to stop runtime services", zap.Error(err))
		return fmt.Errorf("stop runtime services: %w", err)
	}

	if !silentLogs {
		logger.Info("stopped")
	}
	return nil
}
