package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/deps/client"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/internal/banner"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/runtime/cmd/internal/entries"
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	"github.com/wippyai/runtime/cmd/internal/shutdown"
	"go.uber.org/zap"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the runtime from lock file",
	Long: `Start the Wippy runtime environment from wippy.lock file

Loads entries from lock file, runs full pipeline (Override, Disable, Link),
and starts the runtime.

Examples:
  wippy run
  wippy run --override app:gateway:addr=:9090
  wippy run -o app:db:host=localhost -o app:db:port=5432`,
	RunE: runApp,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringSliceP("override", "o", nil, "Override entry values (format: namespace:entry:field=value)")
	runCmd.Flags().StringP("exec", "x", "", "Execute process and exit (format: host/namespace:entry)")
	runCmd.Flags().String("method", "", "Method to call on exec process (default: entry point)")
}

func runApp(cmd *cobra.Command, _ []string) error {
	banner.Print(silentLogs)

	logger, err := clilogger.CreateLogger(clilogger.Config{
		Verbose:      verbose,
		VeryVerbose:  veryVerbose,
		Console:      console,
		Silent:       silentLogs,
		AppStartTime: appStartTime,
	})
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer func() {
		_ = logger.Sync() // Ignore sync errors (typically closed stdout/stderr)
	}()

	logger.Info("initializing runtime")

	cfg, err := loadBootConfig()
	if err != nil {
		logger.Error("failed to load config", zap.Error(err))
		return err
	}

	if cfg == nil {
		cfg = createDefaultConfig()
	}

	overrides, _ := cmd.Flags().GetStringSlice("override")
	if len(overrides) > 0 {
		cfg, err = applyOverrideFlags(cfg, overrides, logger)
		if err != nil {
			logger.Error("failed to apply override flags", zap.Error(err))
			return err
		}
	}

	ctx, err := bootpkg.NewBootstrapContext(logger, cfg)
	if err != nil {
		logger.Error("failed to initialize bootstrap context", zap.Error(err))
		return fmt.Errorf("initialize bootstrap context: %w", err)
	}

	// Initialize registry client for module installation
	registryClient := client.NewRegistryClientFromConfig(boot.GetConfig(ctx))
	ctx = appinit.WithRegistryClient(ctx, registryClient)

	logger = logapi.GetLogger(ctx).Named("run")
	logger.Info("infrastructure initialized")

	components := StandardComponents()
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
	logger.Info("components loaded successfully")

	err = loader.Start(ctx)
	if err != nil {
		logger.Error("start failed", zap.Error(err))
		return fmt.Errorf("failed to start components: %w", err)
	}

	if err := entries.LoadFromLockFile(ctx, logger, verbose); err != nil {
		logger.Error("entry loading failed", zap.Error(err))
		return fmt.Errorf("failed to load entries: %w", err)
	}

	if !silentLogs {
		logger.Info("runtime ready")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Store signal channel for system.exit()
	supervisorapi.SetSignalChannel(ctx, sigChan)

	// Handle --exec flag: launch process and wait for completion
	execSpec, _ := cmd.Flags().GetString("exec")
	if execSpec != "" {
		execMethod, _ := cmd.Flags().GetString("method")
		if err := launchExecProcess(ctx, logger, execSpec, execMethod); err != nil {
			logger.Error("exec launch failed", zap.Error(err))
			return err
		}
	}

	<-sigChan

	// Spawn force-exit handler for second signal
	go func() {
		<-sigChan
		logger.Error("force exit")
		os.Exit(1)
	}()

	if !silentLogs {
		logger.Info("shutting down (press Ctrl+C again to force exit)")
	}

	// Perform shutdown and get exit code
	exitCode := shutdown.Perform(ctx, loader, logger, silentLogs)
	if exitCode != 0 {
		_ = logger.Sync() // Manually sync before exit since defers won't run
		os.Exit(exitCode) //nolint:gocritic // We explicitly sync logger before exit
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
	var opts []boot.ConfigOption

	if verbose || veryVerbose || console {
		loggerCfg := map[string]interface{}{}

		if verbose || veryVerbose {
			loggerCfg["mode"] = "development"
			loggerCfg["level"] = "debug"
		}

		if console {
			loggerCfg["encoding"] = "console"
		}

		if len(loggerCfg) > 0 {
			opts = append(opts, boot.WithSection("logger", loggerCfg))
		}
	}

	if eventStreams {
		opts = append(opts, boot.WithSection("logmanager", map[string]interface{}{
			"stream_to_events": true,
		}))
	}

	if profiler {
		opts = append(opts, boot.WithSection("profiler", map[string]interface{}{
			"enabled": true,
			"address": "localhost:6060",
		}))
	}

	return boot.NewConfig(opts...)
}

func applyOverrideFlags(cfg boot.Config, overrides []string, logger *zap.Logger) (boot.Config, error) {
	overrideMap := make(map[string]interface{})

	// Get existing overrides from config if any
	if cfg != nil {
		sub := cfg.Sub("override")
		if sub != nil {
			for _, key := range sub.Keys() {
				if val, ok := sub.Get(key); ok {
					overrideMap[key] = val
				}
			}
		}
	}

	// Parse and add CLI overrides
	for _, override := range overrides {
		namespace, entry, field, value, err := parseOverride(override)
		if err != nil {
			return nil, fmt.Errorf("invalid override '%s': %w", override, err)
		}

		// Format: namespace:entry:field
		key := fmt.Sprintf("%s:%s:%s", namespace, entry, field)
		overrideMap[key] = value

		if logger != nil {
			logger.Debug("applying override",
				zap.String("key", key),
				zap.String("value", value))
		}
	}

	// Create new config with merged overrides
	opts := []boot.ConfigOption{
		boot.WithSection("override", overrideMap),
	}

	if cfg != nil {
		return bootconfig.Merge(cfg, boot.NewConfig(opts...)), nil
	}

	return boot.NewConfig(opts...), nil
}

func parseOverride(input string) (namespace, entry, field, value string, err error) {
	// Find equals sign to split key=value
	eqIdx := strings.Index(input, "=")
	if eqIdx == -1 {
		return "", "", "", "", fmt.Errorf("missing '=' separator (expected namespace:entry:field=value)")
	}

	keyPart := input[:eqIdx]
	value = input[eqIdx+1:]

	// Find first colon to separate namespace
	firstColonIdx := strings.Index(keyPart, ":")
	if firstColonIdx == -1 {
		return "", "", "", "", fmt.Errorf("missing first ':' separator (expected namespace:entry:field=value)")
	}

	namespace = strings.TrimSpace(keyPart[:firstColonIdx])
	remainder := keyPart[firstColonIdx+1:]

	if namespace == "" {
		return "", "", "", "", fmt.Errorf("empty namespace")
	}

	// Find second colon to separate entry from field
	secondColonIdx := strings.Index(remainder, ":")
	if secondColonIdx == -1 {
		return "", "", "", "", fmt.Errorf("missing second ':' separator (expected namespace:entry:field=value)")
	}

	entry = strings.TrimSpace(remainder[:secondColonIdx])
	field = strings.TrimSpace(remainder[secondColonIdx+1:])

	if entry == "" {
		return "", "", "", "", fmt.Errorf("empty entry name")
	}

	if field == "" {
		return "", "", "", "", fmt.Errorf("empty field")
	}

	return namespace, entry, field, value, nil
}

// parseExecSpec parses "host/namespace:entry" format for --exec flag
// Uses / to separate host (since host IDs can contain colons like "node:control")
func parseExecSpec(spec string) (hostID, namespace, entry string, err error) {
	// Find slash to separate host from source
	slashIdx := strings.Index(spec, "/")
	if slashIdx == -1 {
		return "", "", "", fmt.Errorf("missing '/' separator (expected host/namespace:entry)")
	}

	hostID = strings.TrimSpace(spec[:slashIdx])
	remainder := spec[slashIdx+1:]

	if hostID == "" {
		return "", "", "", fmt.Errorf("empty host ID")
	}

	// Find colon to separate namespace from entry
	colonIdx := strings.Index(remainder, ":")
	if colonIdx == -1 {
		return "", "", "", fmt.Errorf("missing ':' separator (expected host/namespace:entry)")
	}

	namespace = strings.TrimSpace(remainder[:colonIdx])
	entry = strings.TrimSpace(remainder[colonIdx+1:])

	if namespace == "" {
		return "", "", "", fmt.Errorf("empty namespace")
	}

	if entry == "" {
		return "", "", "", fmt.Errorf("empty entry name")
	}

	return hostID, namespace, entry, nil
}

// launchExecProcess launches a process and triggers shutdown on completion
func launchExecProcess(ctx context.Context, logger *zap.Logger, execSpec, method string) error {
	hostID, namespace, entry, err := parseExecSpec(execSpec)
	if err != nil {
		return fmt.Errorf("invalid exec spec: %w", err)
	}

	manager := process.GetManager(ctx)
	if manager == nil {
		return fmt.Errorf("process manager not available")
	}

	source := registry.NewID(namespace, entry)

	start := &process.Start{
		HostID: hostID,
		Source: source,
	}

	pid, err := manager.Start(ctx, start)
	if err != nil {
		return fmt.Errorf("failed to start process on host %s: %w", hostID, err)
	}

	logger.Debug("exec process started",
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("source", source.String()),
		zap.String("method", method))

	return nil
}

// interpretExitCode extracts an exit code from a process result
func interpretExitCode(result *runtime.Result) int {
	if result == nil {
		return 0
	}

	if result.Error != nil {
		return 1
	}

	if result.Value == nil {
		return 0
	}

	// Try to interpret result as a number
	switch v := result.Value.Data().(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}
