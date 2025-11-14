package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/boot/build"
	"github.com/ponyruntime/pony/boot/build/stages"
	"github.com/ponyruntime/pony/boot/pack"
	"github.com/ponyruntime/pony/cmd/wippy/version"
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
	packCmd.Flags().StringP("description", "d", "", "pack description")
	packCmd.Flags().StringSliceP("tags", "t", nil, "pack tags")
	packCmd.Flags().StringArrayP("meta", "m", nil, "custom metadata (key=value, supports dotted notation)")
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

	logger.Info("pipeline executed, entries fully linked", zap.Int("entries", len(entries)))

	// Build metadata
	description, _ := cmd.Flags().GetString("description")
	tags, _ := cmd.Flags().GetStringSlice("tags")
	metaFlags, _ := cmd.Flags().GetStringArray("meta")

	metadata := regapi.Metadata{
		"wippy_version": version.Version,
		"wippy_commit":  version.Commit,
		"wippy_date":    version.Date,
		"packed_at":     time.Now().UTC().Format(time.RFC3339),
		"entry_count":   len(entries),
	}

	if description != "" {
		metadata["description"] = description
	}
	if len(tags) > 0 {
		metadata["tags"] = tags
	}

	// Parse and merge custom metadata
	if err := parseMetadataFlags(metaFlags, metadata, logger); err != nil {
		return fmt.Errorf("parse metadata: %w", err)
	}

	// Create packer and pack entries
	packer := pack.New(app.Transcoder)
	if err := packer.Pack(entries, outputFile, metadata); err != nil {
		return fmt.Errorf("pack entries: %w", err)
	}

	fileInfo, _ := os.Stat(outputFile)
	logger.Info("pack created successfully",
		zap.String("output", outputFile),
		zap.Int("entries", len(entries)),
		zap.Int64("size_bytes", fileInfo.Size()),
		zap.String("version", metadata.StringValue("wippy_version")))

	return nil
}

func parseMetadataFlags(metaFlags []string, metadata regapi.Metadata, logger *zap.Logger) error {
	for _, flag := range metaFlags {
		parts := strings.SplitN(flag, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid metadata format %q, expected key=value", flag)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key == "" {
			return fmt.Errorf("empty metadata key in %q", flag)
		}

		if strings.HasPrefix(key, "wippy.") || strings.HasPrefix(key, "system.") {
			return fmt.Errorf("reserved metadata namespace: %s", key)
		}

		parsedValue := parseMetadataValue(value)
		metadata[key] = parsedValue

		logger.Debug("added custom metadata",
			zap.String("key", key),
			zap.Any("value", parsedValue))
	}

	return nil
}

func parseMetadataValue(value string) any {
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}

	if num, err := strconv.ParseInt(value, 10, 64); err == nil {
		return num
	}

	if num, err := strconv.ParseFloat(value, 64); err == nil {
		return num
	}

	return value
}
