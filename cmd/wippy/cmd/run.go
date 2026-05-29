// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/deps/client"
	bootextensions "github.com/wippyai/runtime/boot/extensions"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/internal/banner"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/runtime/cmd/internal/entries"
	"github.com/wippyai/runtime/cmd/internal/shutdown"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	supervisorpkg "github.com/wippyai/runtime/system/supervisor"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var runCmd = &cobra.Command{
	Use:   "run [command|file.wapp|org/module[@version]]",
	Short: "Start the runtime or execute a command",
	Long: `Start the Wippy runtime environment.

Without arguments, starts the full runtime from wippy.lock.
With a command name, executes the matching process entry.
With a .wapp file, runs directly from the pack file.
With an org/module reference, downloads from hub and runs.

Use 'wippy run list' to see available commands.

Examples:
  wippy run                                 # Start the runtime
  wippy run list                            # List available commands
  wippy run test                            # Run the test command
  wippy run -x app:cli                      # Execute specific process
  wippy run snapshot.wapp                   # Run from pack file
  wippy run acme/http                       # Run latest from hub
  wippy run acme/http@1.2.3                 # Run specific version
  wippy run acme/http@latest                # Run latest label`,
	Args:               cobra.ArbitraryArgs,
	DisableFlagParsing: false,
	RunE:               runApp,
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available commands",
	Long:  `List all process entries that have command metadata defined.`,
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.AddCommand(listCmd)
	runCmd.Flags().StringSliceP("override", "o", nil, "Override entry values (format: namespace:entry:field=value)")
	runCmd.Flags().StringP("exec", "x", "", "Execute process and exit (format: namespace:entry)")
	runCmd.Flags().String("host", "", "Terminal host ID for exec (auto-detected if only one terminal.host exists)")
	runCmd.Flags().String("registry", "", "Registry URL for hub modules (default: from credentials)")
}

// commandMeta represents the command metadata from entry.Meta
type commandMeta struct {
	Name  string `json:"name"`
	Short string `json:"short"`
	Main  bool   `json:"main"`
}

// runApp is the primary `wippy run` execution flow.
// It resolves invocation mode (runtime, pack, hub module, exec), bootstraps
// components, loads registry entries, and blocks until shutdown.
func runApp(cmd *cobra.Command, args []string) error {
	memLimit := initMemoryLimit()

	var commandName string
	var commandArgs []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		commandName = args[0]
		commandArgs = args[1:]
	}

	execSpec := ""
	execHost := ""
	registryURL := ""
	if cmd != nil {
		execSpec, _ = cmd.Flags().GetString("exec")
		execHost, _ = cmd.Flags().GetString("host")
		registryURL, _ = cmd.Flags().GetString("registry")
	}

	if commandName != "" {
		if strings.HasSuffix(commandName, ".wapp") {
			return runFromPackFile(cmd, commandName, commandArgs)
		}

		if isHubModuleRef(commandName) {
			packPaths, err := downloadHubModule(cmd.Context(), commandName, registryURL)
			if err != nil {
				return err
			}
			return runFromPackFiles(cmd, packPaths, commandArgs)
		}
	}

	if execSpec != "" || commandName != "" {
		flagsChanged := cmd != nil && (cmd.Flags().Changed("silent") || cmd.Flags().Changed("verbose") || cmd.Flags().Changed("very-verbose") || cmd.Flags().Changed("console"))
		if !flagsChanged {
			silentLogs = true
		}
	}

	banner.Print(silentLogs)

	logger, err := createCommandLogger()
	if err != nil {
		return NewCreateLoggerError(err)
	}
	defer func() {
		_ = logger.Sync()
	}()

	logger.Info("initializing runtime", zap.String("memory_limit", formatBytes(memLimit)))

	cfg, err := loadRuntimeConfig(cmd, logger)
	if err != nil {
		logger.Error("failed to resolve runtime config", zap.Error(err))
		return err
	}

	ctx, err := bootpkg.NewBootstrapContext(logger, cfg)
	if err != nil {
		logger.Error("failed to initialize bootstrap context", zap.Error(err))
		return NewInitializeBootstrapContextError(err)
	}
	ctx = moduleapi.WithSourceRootRegistry(ctx)

	registryClient := client.NewRegistryClientFromConfig(boot.GetConfig(ctx))
	ctx = appinit.WithRegistryClient(ctx, registryClient)

	logger = logapi.GetLogger(ctx).Named("run")
	logger.Info("infrastructure initialized")

	// Create embed registry for fs.embed support with .wapp modules
	embedReg := embedpkg.NewRegistry()
	ctx = embedapi.WithRegistry(ctx, embedReg)
	defer embedReg.Close()

	components := StandardComponents()
	ctx, extensionComponents, err := loadExtensionComponents(ctx, logger, components)
	if err != nil {
		logger.Error("failed to load extensions", zap.Error(err))
		return err
	}

	components = append(components, extensionComponents...)
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

	sigChan := setupSupervisorSignalChannel(ctx)
	defer signal.Stop(sigChan)

	err = loader.Start(ctx)
	if err != nil {
		logger.Error("start failed", zap.Error(err))
		return NewStartComponentsError(err)
	}

	if err := entries.LoadFromLockFile(ctx, logger); err != nil {
		logger.Error("entry loading failed", zap.Error(err))
		return err
	}

	if !silentLogs {
		logger.Info("runtime ready")
	}

	// Resolve command name to entry ID
	if commandName != "" {
		entryID, err := resolveCommandToEntry(ctx, commandName)
		if err != nil {
			return err
		}
		execSpec = entryID
		args = commandArgs
	}

	// Handle exec: launch process and wait for completion
	if execSpec != "" {
		if err := launchExecProcess(ctx, logger, execSpec, execHost, args); err != nil {
			logger.Error("exec launch failed", zap.Error(err))
			return err
		}
	}

	waitForShutdownSignal(sigChan, logger, nil)

	exitCode := shutdown.Perform(ctx, loader, logger, silentLogs)
	if exitCode != 0 {
		_ = logger.Sync()
		os.Exit(exitCode)
	}

	return nil
}

// resolveCommandToEntry finds an entry with meta.command.name matching the given name
func resolveCommandToEntry(ctx context.Context, name string) (string, error) {
	reg := registry.GetRegistry(ctx)
	if reg == nil {
		return "", fmt.Errorf("registry not available")
	}

	allEntries, err := reg.GetAllEntries()
	if err != nil {
		return "", fmt.Errorf("failed to query registry for commands: %w", err)
	}

	for _, e := range allEntries {
		if !isProcessKind(e.Kind) {
			continue
		}

		cmdMeta := extractCommandMeta(e.Meta)
		if cmdMeta != nil && cmdMeta.Name == name {
			return e.ID.String(), nil
		}
	}

	return "", fmt.Errorf("command %q not found. Use 'wippy run list' to see available commands", name)
}

// loadRuntimeConfig resolves the effective runtime configuration for run-like
// commands by merging:
// 1) config file/defaults,
// 2) CLI logger/event-stream overrides,
// 3) explicit -o override fields.
func loadRuntimeConfig(cmd *cobra.Command, logger *zap.Logger) (boot.Config, error) {
	return loadRuntimeConfigWithDefaults(cmd, logger, nil)
}

// loadRuntimeConfigWithDefaults resolves runtime configuration like
// loadRuntimeConfig, but first seeds it with optional runtime defaults.
// Defaults are applied with lower precedence than file and CLI settings.
func loadRuntimeConfigWithDefaults(cmd *cobra.Command, logger *zap.Logger, runtimeDefaults boot.Config) (boot.Config, error) {
	cfg, err := loadBootConfig()
	if err != nil {
		return nil, err
	}

	if cfg == nil {
		cfg = createDefaultConfig()
	}

	if runtimeDefaults != nil {
		cfg = bootconfig.Merge(runtimeDefaults, cfg)
	}

	cfg = applyCLIOverrides(cfg)

	if cmd == nil {
		return cfg, nil
	}

	overrides, _ := cmd.Flags().GetStringSlice("override")
	if len(overrides) == 0 {
		return cfg, nil
	}

	return applyOverrideFlags(cfg, overrides, logger)
}

// isProcessKind returns true if the entry kind is a process type (lua or wasm).
func isProcessKind(kind registry.Kind) bool {
	return strings.HasPrefix(kind, "process.")
}

// extractCommandMeta extracts command metadata from entry.Meta
func extractCommandMeta(meta map[string]any) *commandMeta {
	if meta == nil {
		return nil
	}

	cmdData, ok := meta["command"]
	if !ok {
		return nil
	}

	cmdMap, ok := cmdData.(map[string]any)
	if !ok {
		return nil
	}

	name, _ := cmdMap["name"].(string)
	if name == "" {
		return nil
	}

	short, _ := cmdMap["short"].(string)
	main, _ := cmdMap["main"].(bool)
	return &commandMeta{Name: name, Short: short, Main: main}
}

// runList prints all command-enabled process entries from resolved lock modules.
func runList(cmd *cobra.Command, _ []string) error {
	silentLogs = true

	app, err := appinit.Init(cmd.Context(), verbose, veryVerbose, console, silentLogs, appStartTime)
	if err != nil {
		return NewInitAppError(err)
	}

	lockPath, lockObj, err := loadValidatedLock(".", defaultLockFile)
	if err != nil {
		return err
	}

	var commands []struct {
		Name    string
		Short   string
		EntryID string
	}

	allEntries, err := ensureModulesAndLoadEntries(app.Ctx, lockPath, lockObj, app.Logger.Named("list"), false)
	if err != nil {
		return err
	}

	for _, e := range allEntries {
		if !isProcessKind(e.Kind) {
			continue
		}

		cmdMeta := extractCommandMeta(e.Meta)
		if cmdMeta == nil {
			continue
		}

		commands = append(commands, struct {
			Name    string
			Short   string
			EntryID string
		}{
			Name:    cmdMeta.Name,
			Short:   cmdMeta.Short,
			EntryID: e.ID.String(),
		})
	}

	if len(commands) == 0 {
		fmt.Println("No commands found.")
		fmt.Println("\nTo define a command, add 'command' to entry meta:")
		fmt.Println("  meta:")
		fmt.Println("    command:")
		fmt.Println("      name: test")
		fmt.Println("      short: Run tests")
		return nil
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fmt.Println(titleStyle.Render("Available commands:"))
	fmt.Println()

	for _, c := range commands {
		fmt.Printf("  %s", nameStyle.Render(c.Name))
		if c.Short != "" {
			fmt.Printf("  %s", c.Short)
		}
		fmt.Printf("  %s\n", dimStyle.Render("("+c.EntryID+")"))
	}

	fmt.Println()
	fmt.Println(dimStyle.Render("Run with: wippy run <command>"))

	return nil
}

// loadBootConfig reads config from file, applies defaults, and injects boot
// metadata (config path + directory) into the effective config.
func loadBootConfig() (boot.Config, error) {
	cfgPath := configFile
	if cfgPath == "" {
		cfgPath = defaultConfigFile
	}
	cfgPathAbs, err := filepath.Abs(cfgPath)
	if err != nil {
		cfgPathAbs = cfgPath
	}
	configMeta := boot.NewConfig(boot.WithSection("boot", map[string]any{
		"config_path": cfgPathAbs,
		"config_dir":  filepath.Dir(cfgPathAbs),
	}))

	cfg, err := bootconfig.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	defaults := createDefaultConfig()
	if cfg == nil {
		return bootconfig.Merge(defaults, configMeta), nil
	}

	return bootconfig.Merge(bootconfig.Merge(defaults, cfg), configMeta), nil
}

// createDefaultConfig returns the baseline config implied by global CLI flags.
func createDefaultConfig() boot.Config {
	var opts []boot.ConfigOption

	if profiler {
		opts = append(opts, boot.WithSection("profiler", map[string]any{
			"enabled": true,
			"address": "localhost:6060",
		}))
	}

	return boot.NewConfig(opts...)
}

// loadExtensionComponents loads extension components while reserving existing
// component names so extensions cannot shadow built-ins.
func loadExtensionComponents(ctx context.Context, logger *zap.Logger, reserved []boot.Component) (context.Context, []boot.Component, error) {
	reservedNames := make(map[string]struct{}, len(reserved))
	for _, comp := range reserved {
		if comp == nil {
			continue
		}
		name := comp.Name()
		if name == "" {
			continue
		}
		reservedNames[name] = struct{}{}
	}

	next, res, err := bootextensions.LoadWithReserved(ctx, boot.GetConfig(ctx), reservedNames)
	if err != nil {
		return ctx, nil, err
	}
	if next != nil {
		ctx = next
	}

	if logger != nil && len(res.Extensions) > 0 {
		names := make([]string, 0, len(res.Extensions))
		for _, p := range res.Extensions {
			names = append(names, p.Name)
		}
		logger.Info("extensions loaded", zap.Int("count", len(res.Extensions)), zap.Strings("extensions", names))
	}

	return ctx, res.Components, nil
}

// applyCLIOverrides converts global runtime flags into boot config overrides.
func applyCLIOverrides(cfg boot.Config) boot.Config {
	var opts []boot.ConfigOption

	if verbose || veryVerbose || console {
		loggerCfg := map[string]any{}

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

	logmanagerCfg := map[string]any{}
	if verbose || veryVerbose {
		logmanagerCfg["min_level"] = int(zapcore.DebugLevel)
	} else {
		logmanagerCfg["min_level"] = int(zapcore.InfoLevel)
	}

	if eventStreams {
		logmanagerCfg["stream_to_events"] = true
	}

	opts = append(opts, boot.WithSection("logmanager", logmanagerCfg))

	return bootconfig.Merge(cfg, boot.NewConfig(opts...))
}

// applyOverrideFlags parses repeated `-o namespace:entry:field=value` flags and
// stores them under config.override.
func applyOverrideFlags(cfg boot.Config, overrides []string, logger *zap.Logger) (boot.Config, error) {
	overrideMap := make(map[string]any)

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

	for _, override := range overrides {
		namespace, entry, field, value, err := parseOverride(override)
		if err != nil {
			return nil, NewInvalidOverrideError(override, err)
		}

		key := fmt.Sprintf("%s:%s:%s", namespace, entry, field)
		parsedValue := parseOverrideValue(value)
		overrideMap[key] = parsedValue

		if logger != nil {
			logger.Debug("applying override",
				zap.String("key", key),
				zap.Any("value", parsedValue))
		}
	}

	opts := []boot.ConfigOption{
		boot.WithSection("override", overrideMap),
	}

	if cfg != nil {
		return bootconfig.Merge(cfg, boot.NewConfig(opts...)), nil
	}

	return boot.NewConfig(opts...), nil
}

func parseOverrideValue(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}

	switch strings.ToLower(trimmed) {
	case "true":
		return true
	case "false":
		return false
	}

	if i, err := strconv.ParseInt(trimmed, 10, 0); err == nil && strconv.FormatInt(i, 10) == trimmed {
		return int(i)
	}

	if strings.ContainsAny(trimmed, ".eE") {
		if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return f
		}
	}

	return raw
}

// parseOverride parses `namespace:entry:field=value`.
func parseOverride(input string) (namespace, entry, field, value string, err error) {
	eqIdx := strings.Index(input, "=")
	if eqIdx == -1 {
		return "", "", "", "", NewMissingSeparatorError("=", "namespace:entry:field=value")
	}

	keyPart := input[:eqIdx]
	value = input[eqIdx+1:]

	firstColonIdx := strings.Index(keyPart, ":")
	if firstColonIdx == -1 {
		return "", "", "", "", NewMissingSeparatorError(":", "namespace:entry:field=value")
	}

	namespace = strings.TrimSpace(keyPart[:firstColonIdx])
	remainder := keyPart[firstColonIdx+1:]

	if namespace == "" {
		return "", "", "", "", NewEmptyFieldError("namespace")
	}

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

// parseExecSpec parses `namespace:entry`.
func parseExecSpec(spec string) (namespace, entry string, err error) {
	colonIdx := strings.Index(spec, ":")
	if colonIdx == -1 {
		return "", "", NewMissingSeparatorError(":", "namespace:entry")
	}

	namespace = strings.TrimSpace(spec[:colonIdx])
	entry = strings.TrimSpace(spec[colonIdx+1:])

	if namespace == "" {
		return "", "", NewEmptyFieldError("namespace")
	}

	if entry == "" {
		return "", "", NewEmptyFieldError("entry name")
	}

	return namespace, entry, nil
}

// findTerminalHost resolves terminal host automatically when --host is omitted.
func findTerminalHost(ctx context.Context) (string, error) {
	reg := registry.GetRegistry(ctx)
	if reg == nil {
		return "", fmt.Errorf("registry not available")
	}

	allEntries, err := reg.GetAllEntries()
	if err != nil {
		return "", fmt.Errorf("failed to query registry for terminal hosts: %w", err)
	}

	var hosts []string
	for _, e := range allEntries {
		if e.Kind == "terminal.host" {
			hosts = append(hosts, e.ID.String())
		}
	}

	if len(hosts) == 0 {
		return "", fmt.Errorf("no terminal.host found in registry")
	}
	if len(hosts) > 1 {
		return "", fmt.Errorf("multiple terminal hosts found (%s), use --host to specify", strings.Join(hosts, ", "))
	}
	return hosts[0], nil
}

// launchExecProcess starts a process from exec spec and forwards CLI args
// as string payloads.
func launchExecProcess(ctx context.Context, logger *zap.Logger, execSpec, hostID string, args []string) error {
	namespace, entry, err := parseExecSpec(execSpec)
	if err != nil {
		return NewInvalidExecSpecError(err)
	}

	if hostID == "" {
		hostID, err = findTerminalHost(ctx)
		if err != nil {
			return err
		}
	}

	manager := process.GetManager(ctx)
	if manager == nil {
		return ErrProcessManagerNotAvailable
	}

	if err := waitForHostRunning(ctx, hostID); err != nil {
		return err
	}

	source := registry.NewID(namespace, entry)

	var input payload.Payloads
	for _, arg := range args {
		input = append(input, payload.NewString(arg))
	}

	start := &process.Start{
		HostID: hostID,
		Source: source,
		Input:  input,
	}

	pid, err := manager.Start(ctx, start)
	if err != nil {
		return NewStartProcessError(hostID, err)
	}

	logger.Debug("exec process started",
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("source", source.String()),
		zap.Strings("args", args))

	return nil
}

// waitForHostRunning waits until host is both running in supervisor state and
// discoverable in relay node routing.
func waitForHostRunning(ctx context.Context, hostID string) error {
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

		nodeReady := node == nil
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

// setupSupervisorSignalChannel wires OS termination signals into supervisor
// signal handling.
func setupSupervisorSignalChannel(ctx context.Context) chan os.Signal {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	supervisorapi.SetSignalChannel(ctx, sigChan)
	return sigChan
}

// waitForShutdownSignal handles first-signal graceful shutdown and second-signal
// forced process termination.
func waitForShutdownSignal(sigChan chan os.Signal, logger *zap.Logger, onFirstSignal func()) {
	sig := <-sigChan
	logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	if onFirstSignal != nil {
		onFirstSignal()
	}

	go func() {
		<-sigChan
		logger.Error("force exit")
		os.Exit(1)
	}()

	if !silentLogs {
		logger.Info("shutting down (press Ctrl+C again to force exit)")
	}
}
