package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	appbuild "github.com/ponyruntime/pony/cmd/runner/app"
	requirementresolver2 "github.com/ponyruntime/pony/deps/requirementresolver"
	"github.com/ponyruntime/pony/internal/runtimeconfig"
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
	Long:  "Load application state from JSON dump file and run the application",
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		dumpFile, _ := cmd.Flags().GetString("dump")
		if dumpFile == "" {
			return fmt.Errorf("dump file path is required")
		}

		runtimeConfigFlags, _ := cmd.Flags().GetStringSlice("runtime-config")
		runtimeCfg := runtimeconfig.New()

		for _, configEntry := range runtimeConfigFlags {
			if err := runtimeCfg.SetFromString(configEntry); err != nil {
				logger.Error("failed to parse runtime configuration",
					zap.String("entry", configEntry),
					zap.Error(err))
				return fmt.Errorf("invalid runtime configuration '%s': %w", configEntry, err)
			}
		}

		if len(runtimeConfigFlags) > 0 {
			logger.Info("Loaded runtime configuration",
				zap.Int("entries", len(runtimeConfigFlags)),
				zap.Strings("namespaces", runtimeCfg.GetAllNamespaces()))
		}

		logger.Info("Loading state from dump file", zap.String("file", dumpFile))

		dumpData, err := os.ReadFile(dumpFile)
		if err != nil {
			return fmt.Errorf("failed to read dump file: %w", err)
		}

		var serializable SerializableState
		if err := json.Unmarshal(dumpData, &serializable); err != nil {
			return fmt.Errorf("failed to unmarshal dump file: %w", err)
		}

		logger.Info("Unmarshaled dump file",
			zap.Int("entries_count", len(serializable.Entries)),
			zap.Int("dump_size", len(dumpData)))

		dtt := transcoder.GlobalTranscoder()
		json2.Register(dtt)
		yaml.Register(dtt)
		lua.Register(dtt)

		entries := make([]registry.Entry, len(serializable.Entries))
		for i, se := range serializable.Entries {
			entry, err := convertFromSerializableEntry(se, dtt, logger)
			if err != nil {
				return fmt.Errorf("failed to convert entry %s: %w", se.ID, err)
			}
			entries[i] = entry
		}

		logger.Info("Loaded entries from dump", zap.Int("count", len(entries)))

		logger.Info("Resolving module definitions and dependencies")
		resolver := requirementresolver2.NewResolver(logger.Named("requirement-resolver"))
		entries, err = resolver.ResolveModuleDefinitions(entries)
		if err != nil {
			return fmt.Errorf("failed to resolve module definitions: %w", err)
		}

		// Verify migrations have target_db
		for _, entry := range entries {
			if strings.Contains(entry.ID.String(), "migration") && strings.Contains(entry.ID.String(), "wippy.session") {
				logger.Info("Checking migration after resolution",
					zap.String("id", entry.ID.String()),
					zap.Any("meta", entry.Meta))
			}
		}

		if runtimeCfg != nil {
			entries, err = applyRuntimeConfigOverrides(entries, runtimeCfg, logger)
			if err != nil {
				return fmt.Errorf("failed to apply runtime config overrides: %w", err)
			}
		}

		boot, err := regtop.NewStateBuilder(logger).BuildDelta(registry.State{}, entries)
		if err != nil {
			return fmt.Errorf("failed to build state delta: %w", err)
		}

		enableProfiling, _ := cmd.Flags().GetBool("profiling")
		clusterEnabled, _ := cmd.Flags().GetBool("cluster")
		clusterName, _ := cmd.Flags().GetString("cluster-name")
		clusterBind, _ := cmd.Flags().GetString("cluster-bind")
		clusterPort, _ := cmd.Flags().GetInt("cluster-port")
		clusterJoin, _ := cmd.Flags().GetString("cluster-join")
		clusterSecret, _ := cmd.Flags().GetString("cluster-secret")
		clusterSecretFile, _ := cmd.Flags().GetString("cluster-secret-file")
		clusterAdvertise, _ := cmd.Flags().GetString("cluster-advertise")

		if clusterName == "" {
			if hostname, err := os.Hostname(); err == nil {
				clusterName = hostname
			} else {
				logger.Error("failed to get hostname and no cluster name provided", zap.Error(err))
				os.Exit(1)
			}
		}

		if clusterEnabled {
			if clusterSecret != "" && clusterSecretFile != "" {
				logger.Error("cannot specify both --cluster-secret and --cluster-secret-file")
				os.Exit(1)
			}
		}

		consoleLogging, eventStreaming := GetLoggingConfig()
		minLevel := GetVerboseLevel()

		app, err := appbuild.NewApp(
			logger,
			appbuild.WithPaths(".", "", "", ".", false),
			appbuild.WithLogging(consoleLogging, eventStreaming, minLevel),
			appbuild.WithProfiling(enableProfiling),
			appbuild.WithCluster(clusterEnabled, clusterName, clusterBind, clusterPort, clusterJoin, clusterSecret, clusterSecretFile, clusterAdvertise),
			appbuild.WithRuntimeConfig(runtimeCfg),
		)
		if err != nil {
			logger.Error("failed to create application", zap.Error(err))
			os.Exit(1)
		}

		if err := app.Initialize(); err != nil {
			logger.Error("failed to initialize application", zap.Error(err))
			os.Exit(1)
		}

		runtime.GC()

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()

		if err := startAppWithState(ctx, app, boot, logger); err != nil {
			logger.Error("failed to start application", zap.Error(err))
			os.Exit(1)
		}

		app.StartProfiler()

		logger.Info("Application started successfully from dump")

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		sig := <-sigChan
		logger.Info("received shutdown signal, starting graceful shutdown", zap.String("signal", sig.String()))

		go func() {
			sig := <-sigChan
			logger.Warn("received second shutdown signal, forcing immediate shutdown", zap.String("signal", sig.String()))
			app.ForceShutdown()
		}()

		if err := app.Shutdown(); err != nil {
			logger.Error("error during shutdown", zap.Error(err))
			os.Exit(1)
		}

		logger.Info("shutdown completed")
		return nil
	},
}

func convertFromSerializableEntry(se SerializableEntry, dtt payload.Transcoder, logger *zap.Logger) (registry.Entry, error) {
	meta := make(registry.Metadata)
	for k, v := range se.Meta {
		meta[k] = v
	}

	var payloadData payload.Payload
	if se.Data != nil {
		payloadData = payload.New(se.Data)
	}

	entry := registry.Entry{
		ID:   registry.ParseID(se.ID),
		Kind: se.Kind,
		Meta: meta,
		Data: payloadData,
	}

	return entry, nil
}

func startAppWithState(ctx context.Context, app *appbuild.App, boot registry.ChangeSet, logger *zap.Logger) error {
	return app.StartWithState(ctx, boot)
}

func applyRuntimeConfigOverrides(entries []registry.Entry, runtimeCfg *runtimeconfig.Config, logger *zap.Logger) ([]registry.Entry, error) {
	namespaces := runtimeCfg.GetAllNamespaces()
	if len(namespaces) == 0 {
		return entries, nil
	}

	modifiedCount := 0
	for i := range entries {
		entry := &entries[i]

		for _, ns := range namespaces {
			if entry.ID.NS != ns {
				continue
			}

			nsConfig, exists := runtimeCfg.GetNamespace(ns)
			if !exists {
				continue
			}

			for entryName, entryConfig := range nsConfig {
				if entry.ID.Name != entryName {
					continue
				}

				if err := applyEntryRuntimeConfig(entry, entryConfig); err != nil {
					return entries, fmt.Errorf("entry %s: %w", entry.ID.String(), err)
				}

				modifiedCount++
				break
			}
		}
	}

	logger.Debug("Applied runtime configuration overrides",
		zap.Int("namespaces", len(namespaces)),
		zap.Int("entries_modified", modifiedCount))

	return entries, nil
}

func applyEntryRuntimeConfig(entry *registry.Entry, entryConfig runtimeconfig.EntryConfig) error {
	for key, value := range entryConfig {
		fieldPath := key
		if err := applyFieldPathToEntry(entry, fieldPath, value); err != nil {
			return err
		}
	}
	return nil
}

func applyFieldPathToEntry(targetEntry *registry.Entry, fieldPath string, value string) error {
	valueStr := value

	jqQuery := ".data"
	if fieldPath != "" {
		switch {
		case fieldPath[0] == '.':
			jqQuery = fieldPath
		case len(fieldPath) > 5 && fieldPath[:5] == "meta.":
			jqQuery = "." + fieldPath
		case len(fieldPath) > 5 && fieldPath[:5] == "data.":
			jqQuery = "." + fieldPath
		default:
			jqQuery = ".data." + fieldPath
		}
	}

	entryCopy := *targetEntry
	entriesSlice := []registry.Entry{entryCopy}

	err := requirementresolver2.ApplyPathValueToEntriesWithGojq(jqQuery, valueStr, entriesSlice)
	if err != nil {
		return err
	}

	*targetEntry = entriesSlice[0]
	return nil
}

func init() {
	rootCmd.AddCommand(runDumpCmd)

	runDumpCmd.Flags().StringP("dump", "d", "", "path to state dump file")
	runDumpCmd.Flags().BoolP("profiling", "p", false, "enable performance profiling")
	runDumpCmd.Flags().StringSliceP("runtime-config", "r", []string{}, "runtime configuration in format namespace:entry:field=value")

	runDumpCmd.Flags().BoolP("cluster", "C", false, "enable cluster membership")
	runDumpCmd.Flags().StringP("cluster-name", "n", "", "cluster node name (defaults to hostname)")
	runDumpCmd.Flags().String("cluster-bind", "0.0.0.0", "cluster bind address")
	runDumpCmd.Flags().Int("cluster-port", 7946, "cluster bind port")
	runDumpCmd.Flags().StringP("cluster-join", "j", "", "comma-separated addresses to join")
	runDumpCmd.Flags().String("cluster-secret", "", "cluster secret key")
	runDumpCmd.Flags().String("cluster-secret-file", "", "path to file containing cluster secret key")
	runDumpCmd.Flags().String("cluster-advertise", "", "cluster advertise IP address")

	_ = runDumpCmd.MarkFlagRequired("dump")
}
