package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	regapi "github.com/ponyruntime/pony/api/registry"
	bootpkg "github.com/ponyruntime/pony/boot"
	cli "github.com/ponyruntime/pony/boot/cli"
	"github.com/ponyruntime/pony/boot/pack"
	regtop "github.com/ponyruntime/pony/system/registry/topology"
	"github.com/spf13/cobra"
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

	ctx, err := bootpkg.NewInfrastructure(logger, cfg)
	if err != nil {
		logger.Error("failed to initialize infrastructure", zap.Error(err))
		return fmt.Errorf("initialize infrastructure: %w", err)
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

	// Bridge file loader from AppContext to CLI context for compatibility
	fileLdr := boot.GetLoader(ctx)
	if fileLdr != nil {
		ctx = cli.WithLoader(ctx, fileLdr)
	}

	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return fmt.Errorf("transcoder not found")
	}

	packer := pack.New(transcoder)
	allEntries := []regapi.Entry{}

	for _, packFile := range args {
		logger.Info("unpacking", zap.String("file", packFile))

		entries, metadata, err := packer.Unpack(packFile)
		if err != nil {
			return fmt.Errorf("unpack %s: %w", packFile, err)
		}

		if metadata != nil {
			logger.Info("pack metadata",
				zap.String("file", packFile),
				zap.String("wippy_version", metadata.StringValue("wippy_version")),
				zap.String("commit", metadata.StringValue("wippy_commit")),
				zap.String("packed_at", metadata.StringValue("packed_at")),
				zap.Int("entry_count", metadata.IntValue("entry_count")),
				zap.String("description", metadata.StringValue("description")))
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

	err = bootpkg.StartInfrastructure(appCtx)
	if err != nil {
		logger.Error("failed to start infrastructure", zap.Error(err))
		return fmt.Errorf("start infrastructure: %w", err)
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

	err = bootpkg.StopInfrastructure(ctx)
	if err != nil {
		logger.Error("failed to stop infrastructure", zap.Error(err))
		return fmt.Errorf("stop infrastructure: %w", err)
	}

	if !silentLogs {
		logger.Info("stopped")
	}
	return nil
}
