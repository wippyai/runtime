package cmd

import (
	"fmt"
	"os"

	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/boot/build"
	"github.com/ponyruntime/pony/boot/build/stages"
	"github.com/ponyruntime/pony/boot/pack"
	"github.com/ponyruntime/pony/deps/lock"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var packCmd = &cobra.Command{
	Use:   "pack <output.wapp>",
	Short: "Create a snapshot pack of the application state",
	Long: `Load all entries and dependencies, execute full pipeline (override, disable, link),
and serialize to a compressed binary .wapp file.

The pack file contains fully linked entries ready for loading without additional processing.

Examples:
  wippy pack snapshot.wapp
  wippy pack release-v1.2.3.wapp`,
	Args: cobra.ExactArgs(1),
	RunE: runPack,
}

func init() {
	rootCmd.AddCommand(packCmd)

	packCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
}

func runPack(cmd *cobra.Command, args []string) error {
	app, err := InitApp(cmd.Context())
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}

	logger := app.Logger.Named("pack")

	outputFile := args[0]
	lockFile, _ := cmd.Flags().GetString("lock-file")
	folderPath := "."

	lockPath, err := lock.Find(folderPath, lockFile)
	if err != nil {
		return fmt.Errorf("lock file not found: %w", err)
	}

	logger.Info("loading entries from lock file", zap.String("path", lockPath))

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return fmt.Errorf("load lock file: %w", err)
	}

	if err := ensureModulesInstalled(app.Ctx, lockPath, lockFile, logger); err != nil {
		return fmt.Errorf("ensure modules installed: %w", err)
	}

	paths := lockObj.GetLoadPaths()
	logger.Debug("load paths from lock file", zap.Strings("paths", paths))

	entries := []regapi.Entry{}
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			logger.Warn("path not found, skipping", zap.String("path", path))
			continue
		}

		dirFS := os.DirFS(path)
		loadedEntries, err := app.Loader.LoadFS(app.Ctx, dirFS)
		if err != nil {
			return fmt.Errorf("load from %s: %w", path, err)
		}

		logger.Debug("loaded entries from path",
			zap.String("path", path),
			zap.Int("count", len(loadedEntries)))

		entries = append(entries, loadedEntries...)
	}

	logger.Info("loaded entries", zap.Int("count", len(entries)))

	logger.Info("executing full pipeline stages (override, disable, link)")
	pipeline := build.New(
		stages.Override(),
		stages.Disable(),
		stages.Link(),
	)

	if err := pipeline.Execute(app.Ctx, &entries); err != nil {
		return fmt.Errorf("execute pipeline: %w", err)
	}

	// Check for link warnings and display them
	if warnings := stages.GetLinkWarnings(app.Ctx); len(warnings) > 0 {
		logger.Warn("link stage completed with warnings", zap.Int("count", len(warnings)))
		for _, w := range warnings {
			logger.Warn("unresolved requirement",
				zap.String("requirement", w.Requirement),
				zap.String("error", w.Error))
		}
	}

	logger.Info("pipeline executed, entries fully linked", zap.Int("entries", len(entries)))

	// Create packer and pack entries
	packer := pack.New(app.Transcoder)
	if err := packer.Pack(entries, outputFile); err != nil {
		return fmt.Errorf("pack entries: %w", err)
	}

	fileInfo, _ := os.Stat(outputFile)
	logger.Info("pack created successfully",
		zap.String("output", outputFile),
		zap.Int("entries", len(entries)),
		zap.Int64("size_bytes", fileInfo.Size()))

	return nil
}
