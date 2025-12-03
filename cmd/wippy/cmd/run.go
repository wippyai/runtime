package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
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
	supervisorpkg "github.com/wippyai/runtime/system/supervisor"
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

	logger.Info("initializing runtime", zap.String("memory_limit", formatBytes(memLimit)))

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
		return NewInitializeBootstrapContextError(err)
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
		return NewCreateLoaderError(err)
	}

	ctx, err = loader.Load(ctx)
	if err != nil {
		logger.Error("load failed", zap.Error(err))
		return NewLoadComponentsError(err)
	}
	logger.Info("components loaded successfully")

	err = loader.Start(ctx)
	if err != nil {
		logger.Error("start failed", zap.Error(err))
		return NewStartComponentsError(err)
	}

	if err := entries.LoadFromLockFile(ctx, logger, verbose); err != nil {
		logger.Error("entry loading failed", zap.Error(err))
		return NewLoadEntriesError("lock file", err)
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
			return nil, NewInvalidOverrideError(override, err)
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
		return "", "", "", "", NewMissingSeparatorError("=", "namespace:entry:field=value")
	}

	keyPart := input[:eqIdx]
	value = input[eqIdx+1:]

	// Find first colon to separate namespace
	firstColonIdx := strings.Index(keyPart, ":")
	if firstColonIdx == -1 {
		return "", "", "", "", NewMissingSeparatorError(":", "namespace:entry:field=value")
	}

	namespace = strings.TrimSpace(keyPart[:firstColonIdx])
	remainder := keyPart[firstColonIdx+1:]

	if namespace == "" {
		return "", "", "", "", NewEmptyFieldError("namespace")
	}

	// Find second colon to separate entry from field
	secondColonIdx := strings.Index(remainder, ":")
	if secondColonIdx == -1 {
		return "", "", "", "", NewMissingSeparatorError(":", "namespace:entry:field=value")
	}

	entry = strings.TrimSpace(remainder[:secondColonIdx])
	field = strings.TrimSpace(remainder[secondColonIdx+1:])

	if entry == "" {
		return "", "", "", "", NewEmptyFieldError("entry name")
	}

	if field == "" {
		return "", "", "", "", NewEmptyFieldError("field")
	}

	return namespace, entry, field, value, nil
}

// parseExecSpec parses "host/namespace:entry" format for --exec flag
// Uses / to separate host (since host IDs can contain colons like "node:control")
func parseExecSpec(spec string) (hostID, namespace, entry string, err error) {
	// Find slash to separate host from source
	slashIdx := strings.Index(spec, "/")
	if slashIdx == -1 {
		return "", "", "", NewMissingSeparatorError("/", "host/namespace:entry")
	}

	hostID = strings.TrimSpace(spec[:slashIdx])
	remainder := spec[slashIdx+1:]

	if hostID == "" {
		return "", "", "", NewEmptyFieldError("host ID")
	}

	// Find colon to separate namespace from entry
	colonIdx := strings.Index(remainder, ":")
	if colonIdx == -1 {
		return "", "", "", NewMissingSeparatorError(":", "host/namespace:entry")
	}

	namespace = strings.TrimSpace(remainder[:colonIdx])
	entry = strings.TrimSpace(remainder[colonIdx+1:])

	if namespace == "" {
		return "", "", "", NewEmptyFieldError("namespace")
	}

	if entry == "" {
		return "", "", "", NewEmptyFieldError("entry name")
	}

	return hostID, namespace, entry, nil
}

// launchExecProcess launches a process and triggers shutdown on completion
func launchExecProcess(ctx context.Context, logger *zap.Logger, execSpec, method string) error {
	hostID, namespace, entry, err := parseExecSpec(execSpec)
	if err != nil {
		return NewInvalidExecSpecError(err)
	}

	manager := process.GetManager(ctx)
	if manager == nil {
		return ErrProcessManagerNotAvailable
	}

	// Wait for the host service to be running before starting the process
	if err := waitForHostRunning(ctx, logger, hostID); err != nil {
		return err
	}

	source := registry.NewID(namespace, entry)

	start := &process.Start{
		HostID: hostID,
		Source: source,
	}

	pid, err := manager.Start(ctx, start)
	if err != nil {
		return NewStartProcessError(hostID, err)
	}

	logger.Debug("exec process started",
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("source", source.String()),
		zap.String("method", method))

	return nil
}

// waitForHostRunning polls the supervisor until the host service is running
// and the host is registered in the relay node.
func waitForHostRunning(ctx context.Context, logger *zap.Logger, hostID string) error {
	sup, ok := supervisorapi.GetSupervisor(ctx).(*supervisorpkg.Supervisor)
	if !ok || sup == nil {
		return fmt.Errorf("supervisor not available")
	}

	node := relay.GetNode(ctx)

	const (
		pollInterval = 10 * time.Millisecond
		timeout      = 5 * time.Second
	)

	deadline := time.Now().Add(timeout)
	for {
		state, err := sup.GetState(hostID)
		supervisorReady := err == nil && state.Status == supervisorapi.StatusRunning

		// Also check that the host is registered in the relay node
		nodeReady := node == nil // skip check if node not available
		if node != nil {
			_, nodeReady = node.GetHost(hostID)
		}

		if supervisorReady && nodeReady {
			return nil
		}

		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("host %s not found in supervisor: %w", hostID, err)
			}
			if !nodeReady {
				return fmt.Errorf("timeout waiting for host %s to register in node", hostID)
			}
			return fmt.Errorf("timeout waiting for host %s to start (status: %s)", hostID, state.Status)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
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
