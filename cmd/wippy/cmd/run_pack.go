package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/pack"
	"github.com/wippyai/runtime/cmd/internal/banner"
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	"github.com/wippyai/runtime/cmd/internal/shutdown"
	"github.com/wippyai/runtime/service/fs/embed"
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

func runFromPack(_ *cobra.Command, args []string) error {
	// Set memory limit early, before any significant allocations
	memLimit := initMemoryLimit()

	banner.Print(silentLogs)

	logger, err := clilogger.CreateLogger(clilogger.Config{
		Verbose:      verbose,
		VeryVerbose:  veryVerbose,
		Console:      console,
		Silent:       silentLogs,
		AppStartTime: appStartTime,
	})
	if err != nil {
		return NewCreateLoggerError(err)
	}
	defer func() {
		_ = logger.Sync() // Ignore sync errors (typically closed stdout/stderr)
	}()

	logger.Info("loading pack files", zap.Int("count", len(args)), zap.String("memory_limit", formatBytes(memLimit)))

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
		return NewInitializeBootstrapContextError(err)
	}

	logger = logapi.GetLogger(ctx).Named("run-pack")
	logger.Info("infrastructure initialized")

	embedReg := embed.NewRegistry()
	ctx = embedapi.WithRegistry(ctx, embedReg)
	logger.Info("embed registry initialized")

	components := StandardComponents()
	logger.Info("registered components", zap.Int("count", len(components)))

	loader, err := bootpkg.NewLoader(components...)
	if err != nil {
		logger.Error("failed to create loader", zap.Error(err))
		return NewCreateLoaderError(err)
	}

	ctx, err = loader.Load(ctx)
	if err != nil {
		logger.Error("load failed", zap.Error(err))
		return NewLoadComponentsError(err)
	}
	logger.Info("components loaded successfully")

	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return ErrTranscoderNotFound
	}

	var allEntries []regapi.Entry
	openFiles := make([]*os.File, 0, len(args))

	defer func() {
		for _, f := range openFiles {
			_ = f.Close()
		}
	}()

	for _, packFile := range args {
		logger.Info("loading pack", zap.String("file", packFile))

		file, err := os.Open(packFile)
		if err != nil {
			return NewOpenPackFileError(packFile, err)
		}
		openFiles = append(openFiles, file)

		reader, err := pack.NewReader(file, transcoder)
		if err != nil {
			return NewCreatePackReaderError(packFile, err)
		}

		entries, err := reader.GetEntries()
		if err != nil {
			return NewReadEntriesError(packFile, err)
		}

		metadata, err := reader.GetMetadata()
		if err == nil {
			logger.Info("pack metadata",
				zap.String("file", packFile),
				zap.String("wippy_version", metadata.GetString("wippy_version", "")),
				zap.String("commit", metadata.GetString("wippy_commit", "")),
				zap.String("packed_at", metadata.GetString("packed_at", "")),
				zap.Int("entry_count", metadata.GetInt("entry_count", 0)),
				zap.String("description", metadata.GetString("description", "")))
		}

		logger.Info("loaded entries",
			zap.String("file", packFile),
			zap.Int("count", len(entries)))

		resources, err := reader.ListResources()
		if err == nil && len(resources) > 0 {
			logger.Info("loaded embedded resources",
				zap.String("file", packFile),
				zap.Int("count", len(resources)))
			for _, res := range resources {
				logger.Debug("embedded resource",
					zap.String("id", res.ID.String()),
					zap.String("type", res.Type),
					zap.Uint64("size", res.Size))
			}
		}

		if err := embedReg.Register(packFile, reader); err != nil {
			logger.Error("failed to register pack", zap.String("pack", packFile), zap.Error(err))
			return NewRegisterPackError(packFile, err)
		}
		allEntries = append(allEntries, entries...)
	}

	logger.Info("total entries loaded", zap.Int("count", len(allEntries)))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	supervisorapi.SetSignalChannel(ctx, sigChan)

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = bootpkg.StartRuntimeServices(appCtx)
	if err != nil {
		logger.Error("failed to start runtime services", zap.Error(err))
		return NewStartRuntimeServicesError(err)
	}

	err = loader.Start(appCtx)
	if err != nil {
		logger.Error("start failed", zap.Error(err))
		return NewStartComponentsError(err)
	}

	reg := regapi.GetRegistry(appCtx)
	if reg == nil {
		return ErrRegistryNotFound
	}

	resolver := regapi.GetResolver(appCtx)
	if resolver == nil {
		return ErrDependencyResolverNotFound
	}

	logger.Debug("building change set from entries")
	// Use CreateChangeSetFromEntries which properly sorts by dependencies
	changeSet, err := regtop.CreateChangeSetFromEntries(allEntries, resolver)
	if err != nil {
		return NewBuildChangeSetError(err)
	}
	logger.Debug("change set built")

	logger.Info("applying change set to registry", zap.Int("entry_count", len(allEntries)))
	version, err := reg.Apply(appCtx, changeSet)
	if err != nil {
		logger.Error("apply failed", zap.Error(err))
		return NewApplyEntriesError(err)
	}

	logger.Info("entries applied to registry", zap.Any("version", version))

	if !silentLogs {
		logger.Info("runtime ready")
	}

	sig := <-sigChan
	logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	cancel()

	// Spawn force-exit handler for second signal
	go func() {
		sig := <-sigChan
		logger.Error("force exit", zap.String("signal", sig.String()))
		os.Exit(1)
	}()

	if !silentLogs {
		logger.Info("shutting down (press Ctrl+C again to force exit)")
	}

	// Perform shutdown and get exit code
	exitCode := shutdown.Perform(ctx, loader, logger, silentLogs)
	if exitCode != 0 {
		_ = logger.Sync() // Manually sync before exit since defers won't run
		os.Exit(exitCode) //nolint:gocritic // We explicitly sync logger and cancel is called in performShutdown
	}

	return nil
}
