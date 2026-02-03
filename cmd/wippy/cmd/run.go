package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	bootpkg "github.com/wippyai/runtime/boot"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/client"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	bootextensions "github.com/wippyai/runtime/boot/extensions"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/runtime/cmd/internal/banner"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/runtime/cmd/internal/entries"
	clilogger "github.com/wippyai/runtime/cmd/internal/logger"
	"github.com/wippyai/runtime/cmd/internal/shutdown"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	supervisorpkg "github.com/wippyai/runtime/system/supervisor"
	"github.com/wippyai/wapp"
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
}

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
		_ = logger.Sync()
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

	cfg = applyCLIOverrides(cfg)

	if cmd != nil {
		overrides, _ := cmd.Flags().GetStringSlice("override")
		if len(overrides) > 0 {
			cfg, err = applyOverrideFlags(cfg, overrides, logger)
			if err != nil {
				logger.Error("failed to apply override flags", zap.Error(err))
				return err
			}
		}
	}

	ctx, err := bootpkg.NewBootstrapContext(logger, cfg)
	if err != nil {
		logger.Error("failed to initialize bootstrap context", zap.Error(err))
		return NewInitializeBootstrapContextError(err)
	}

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

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	supervisorapi.SetSignalChannel(ctx, sigChan)

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

	<-sigChan

	go func() {
		<-sigChan
		logger.Error("force exit")
		os.Exit(1)
	}()

	if !silentLogs {
		logger.Info("shutting down (press Ctrl+C again to force exit)")
	}

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
		if !strings.HasPrefix(e.Kind, "process.lua") {
			continue
		}

		cmdMeta := extractCommandMeta(e.Meta)
		if cmdMeta != nil && cmdMeta.Name == name {
			return e.ID.String(), nil
		}
	}

	return "", fmt.Errorf("command %q not found. Use 'wippy run list' to see available commands", name)
}

// extractCommandMeta extracts command metadata from entry.Meta
func extractCommandMeta(meta map[string]interface{}) *commandMeta {
	if meta == nil {
		return nil
	}

	cmdData, ok := meta["command"]
	if !ok {
		return nil
	}

	cmdMap, ok := cmdData.(map[string]interface{})
	if !ok {
		return nil
	}

	name, _ := cmdMap["name"].(string)
	if name == "" {
		return nil
	}

	short, _ := cmdMap["short"].(string)
	return &commandMeta{Name: name, Short: short}
}

func runList(cmd *cobra.Command, _ []string) error {
	silentLogs = true

	app, err := appinit.Init(cmd.Context(), verbose, veryVerbose, console, silentLogs, appStartTime)
	if err != nil {
		return NewInitAppError(err)
	}

	lockPath, err := lock.Find(".", defaultLockFile)
	if err != nil {
		return NewLockFileNotFoundError(err)
	}

	if err := entries.EnsureModulesInstalled(app.Ctx, lockPath, app.Logger.Named("list")); err != nil {
		return NewEnsureModulesInstalledError(err)
	}

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockPath, err))
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}

	var commands []struct {
		Name    string
		Short   string
		EntryID string
	}

	allEntries, err := loadEntriesFromLockPaths(app.Ctx, lockObj, app.Logger)
	if err != nil {
		return NewLoadEntriesError(fmt.Sprintf("lock paths (%s)", lockPath), err)
	}

	for _, e := range allEntries {
		if !strings.HasPrefix(e.Kind, "process.lua") {
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

func createDefaultConfig() boot.Config {
	var opts []boot.ConfigOption

	if profiler {
		opts = append(opts, boot.WithSection("profiler", map[string]interface{}{
			"enabled": true,
			"address": "localhost:6060",
		}))
	}

	return boot.NewConfig(opts...)
}

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

func applyCLIOverrides(cfg boot.Config) boot.Config {
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

	logmanagerCfg := map[string]interface{}{}
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

func applyOverrideFlags(cfg boot.Config, overrides []string, logger *zap.Logger) (boot.Config, error) {
	overrideMap := make(map[string]interface{})

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
		overrideMap[key] = value

		if logger != nil {
			logger.Debug("applying override",
				zap.String("key", key),
				zap.String("value", value))
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

var hubModulePattern = regexp.MustCompile(`^([a-z][a-z0-9-]*)/([a-z][a-z0-9-]*)(?:@(.+))?$`)

// findPackCommand finds a command entry in the pack
// If commandName is empty, returns the first available command (preferring "run")
func findPackCommand(ctx context.Context, commandName string) (string, error) {
	reg := registry.GetRegistry(ctx)
	if reg == nil {
		return "", fmt.Errorf("registry not available")
	}

	allEntries, err := reg.GetAllEntries()
	if err != nil {
		return "", fmt.Errorf("failed to query registry for pack commands: %w", err)
	}

	var commands []struct {
		name    string
		entryID string
	}

	for _, e := range allEntries {
		if !strings.HasPrefix(e.Kind, "process.lua") {
			continue
		}

		cmdMeta := extractCommandMeta(e.Meta)
		if cmdMeta == nil {
			continue
		}

		commands = append(commands, struct {
			name    string
			entryID string
		}{name: cmdMeta.Name, entryID: e.ID.String()})
	}

	if len(commands) == 0 {
		return "", nil
	}

	if commandName != "" {
		for _, c := range commands {
			if c.name == commandName {
				return c.entryID, nil
			}
		}
		return "", fmt.Errorf("command %q not found in pack", commandName)
	}

	for _, c := range commands {
		if c.name == "run" {
			return c.entryID, nil
		}
	}

	return commands[0].entryID, nil
}

func isHubModuleRef(s string) bool {
	if strings.HasSuffix(s, ".wapp") {
		return false
	}

	if _, err := os.Stat(s); err == nil {
		return false
	}

	return hubModulePattern.MatchString(s)
}

func downloadHubModule(ctx context.Context, ref string, registryURL string) ([]string, error) {
	matches := hubModulePattern.FindStringSubmatch(ref)
	if matches == nil {
		return nil, fmt.Errorf("invalid hub module reference: %s", ref)
	}

	org := matches[1]
	module := matches[2]
	versionOrLabel := ""
	if len(matches) > 3 {
		versionOrLabel = matches[3]
	}

	projectDir, _ := os.Getwd()
	authCfg := bootauth.NewConfig(projectDir)
	store := bootauth.NewStore(authCfg)

	if registryURL == "" {
		registryURL = store.DefaultRegistry()
	}

	cred, _ := store.Get(registryURL)

	var token string
	if cred != nil {
		token = cred.Token
	}

	client, err := hub.NewClient(hub.Options{
		BaseURL: registryURL,
		Token:   token,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create hub client for %s: %w", registryURL, err)
	}

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	fmt.Printf("%s %s/%s", dimStyle.Render("Resolving dependencies for"), org, module)
	if versionOrLabel != "" {
		fmt.Printf("@%s", versionOrLabel)
	}
	fmt.Println("...")

	downloadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	constraint := ""
	if versionOrLabel != "" {
		if isVersionString(versionOrLabel) {
			constraint = versionOrLabel
		} else {
			constraint = "@" + versionOrLabel
		}
	}

	resolveParams := &hub.ResolveDependenciesParams{
		Roots: []hub.DependencySpec{
			{Org: org, Name: module, Constraint: constraint},
		},
	}

	resolved, err := client.ResolveDependencies(downloadCtx, resolveParams)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve dependencies from %s: %w", registryURL, err)
	}

	if len(resolved.Errors) > 0 {
		details := make([]string, 0, len(resolved.Errors))
		for _, resErr := range resolved.Errors {
			details = append(details, formatResolutionError(resErr))
		}
		return nil, fmt.Errorf("dependency resolution errors (%d): %s", len(resolved.Errors), strings.Join(details, "; "))
	}

	if len(resolved.Modules) == 0 {
		return nil, fmt.Errorf("no modules resolved for %s/%s", org, module)
	}

	fmt.Printf("%s Resolved %d module(s)\n", dimStyle.Render(""), len(resolved.Modules))

	cacheDir := getCacheDir()
	var packPaths []string
	var mainPackPath string

	for _, m := range resolved.Modules {
		moduleName := fmt.Sprintf("%s/%s", m.Org, m.Name)
		packPath := filepath.Join(cacheDir, m.Org, fmt.Sprintf("%s-%s.wapp", m.Name, m.Version))

		if _, err := os.Stat(packPath); err == nil {
			fmt.Printf("%s %s@%s (cached)\n", dimStyle.Render(""), moduleName, m.Version)
		} else {
			fmt.Printf("%s Downloading %s@%s...\n", dimStyle.Render(""), moduleName, m.Version)
			if m.URL == "" {
				return nil, fmt.Errorf("no download URL for %s@%s from %s", moduleName, m.Version, registryURL)
			}
			if err := client.DownloadToFile(downloadCtx, m.URL, packPath); err != nil {
				return nil, fmt.Errorf("failed to download %s@%s from %s to %s: %w", moduleName, m.Version, registryURL, packPath, err)
			}
		}

		if err := updateLockFile(moduleName, m.Version, m.Digest); err != nil {
			fmt.Printf("%s Warning: could not update lock file for %s: %v\n", dimStyle.Render(""), moduleName, err)
		}

		if m.Org == org && m.Name == module {
			mainPackPath = packPath
		} else {
			packPaths = append(packPaths, packPath)
		}
	}

	if mainPackPath == "" {
		return nil, fmt.Errorf("main module %s/%s not found in resolved modules", org, module)
	}

	packPaths = append(packPaths, mainPackPath)

	fmt.Println()
	return packPaths, nil
}

func updateLockFile(moduleName, version, digest string) error {
	lockObj, err := lock.New(defaultLockFile)
	if err != nil {
		return fmt.Errorf("lock file %s: %w", defaultLockFile, err)
	}

	mod := lock.Module{
		Name:    moduleName,
		Version: version,
		Hash:    digest,
	}

	lockObj.SetModule(mod)
	if err := lockObj.Write(); err != nil {
		return fmt.Errorf("lock file %s: %w", lockObj.Path(), err)
	}
	return nil
}

func isVersionString(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == 'v' {
		s = s[1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func getCacheDir() string {
	if cacheDir := os.Getenv("WIPPY_CACHE_DIR"); cacheDir != "" {
		return cacheDir
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		return filepath.Join(homeDir, ".wippy", "cache")
	}

	return filepath.Join(os.TempDir(), "wippy-cache")
}

func runFromPackFile(_ *cobra.Command, packFile string, args []string) error {
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
	defer func() { _ = logger.Sync() }()

	logger.Info("loading pack file", zap.String("file", packFile), zap.String("memory_limit", formatBytes(memLimit)))

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

	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return ErrTranscoderNotFound
	}

	file, err := os.Open(packFile)
	if err != nil {
		return NewOpenPackFileError(packFile, err)
	}

	packReader, err := entries.NewPackReader(file, transcoder)
	if err != nil {
		file.Close()
		return NewCreatePackReaderError(packFile, err)
	}

	if err := embedReg.Register(packFile, packReader.Reader(), file); err != nil {
		file.Close()
		return fmt.Errorf("register embed resources: %w", err)
	}

	packEntries, err := packReader.GetEntries()
	if err != nil {
		return NewReadEntriesError(packFile, err)
	}

	logger.Info("loaded entries from pack", zap.Int("count", len(packEntries)))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	supervisorapi.SetSignalChannel(ctx, sigChan)

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := bootpkg.StartRuntimeServices(appCtx); err != nil {
		logger.Error("failed to start runtime services", zap.Error(err))
		return NewStartRuntimeServicesError(err)
	}

	if err := loader.Start(appCtx); err != nil {
		logger.Error("start failed", zap.Error(err))
		return NewStartComponentsError(err)
	}

	reg := registry.GetRegistry(appCtx)
	if reg == nil {
		return ErrRegistryNotFound
	}

	resolver := registry.GetResolver(appCtx)
	if resolver == nil {
		return ErrDependencyResolverNotFound
	}

	if err := entries.ApplyToRegistry(appCtx, packEntries, resolver, reg, logger); err != nil {
		return err
	}

	if !silentLogs {
		logger.Info("runtime ready")
	}

	// Auto-execute command from pack
	commandName := ""
	if len(args) > 0 {
		commandName = args[0]
		args = args[1:]
	}

	entryID, err := findPackCommand(appCtx, commandName)
	if err != nil {
		logger.Error("failed to find command", zap.Error(err))
		return err
	}

	if entryID != "" {
		if err := launchExecProcess(appCtx, logger, entryID, "", args); err != nil {
			logger.Error("exec launch failed", zap.Error(err))
			return err
		}
	}

	sig := <-sigChan
	logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	cancel()

	go func() {
		<-sigChan
		logger.Error("force exit")
		os.Exit(1)
	}()

	if !silentLogs {
		logger.Info("shutting down (press Ctrl+C again to force exit)")
	}

	exitCode := shutdown.Perform(ctx, loader, logger, silentLogs)
	if exitCode != 0 {
		_ = logger.Sync()
		os.Exit(exitCode)
	}

	return nil
}

func runFromPackFiles(_ *cobra.Command, packFiles []string, args []string) error {
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
	defer func() { _ = logger.Sync() }()

	logger.Info("loading pack files", zap.Strings("files", packFiles), zap.String("memory_limit", formatBytes(memLimit)))

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

	packEntries, err := entries.LoadEntriesFromPaths(ctx, packFiles, logger)
	if err != nil {
		logger.Error("failed to load entries from packs", zap.Error(err))
		return NewLoadEntriesError("pack files", err)
	}

	// Register .wapp pack readers with embed registry for fs.embed support
	for _, pf := range packFiles {
		if filepath.Ext(pf) != ".wapp" {
			continue
		}
		f, err := os.Open(pf)
		if err != nil {
			return fmt.Errorf("open pack for embed: %w", err)
		}

		reader, err := wapp.NewReader(f)
		if err != nil {
			f.Close()
			return fmt.Errorf("read pack for embed: %w", err)
		}

		if err := embedReg.Register(pf, reader, f); err != nil {
			f.Close()
			return fmt.Errorf("register embed resources: %w", err)
		}
	}

	logger.Info("loaded entries from packs", zap.Int("count", len(packEntries)))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	supervisorapi.SetSignalChannel(ctx, sigChan)

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := bootpkg.StartRuntimeServices(appCtx); err != nil {
		logger.Error("failed to start runtime services", zap.Error(err))
		return NewStartRuntimeServicesError(err)
	}

	if err := loader.Start(appCtx); err != nil {
		logger.Error("start failed", zap.Error(err))
		return NewStartComponentsError(err)
	}

	reg := registry.GetRegistry(appCtx)
	if reg == nil {
		return ErrRegistryNotFound
	}

	resolver := registry.GetResolver(appCtx)
	if resolver == nil {
		return ErrDependencyResolverNotFound
	}

	if err := entries.ApplyToRegistry(appCtx, packEntries, resolver, reg, logger); err != nil {
		return err
	}

	if !silentLogs {
		logger.Info("runtime ready")
	}

	commandName := ""
	if len(args) > 0 {
		commandName = args[0]
		args = args[1:]
	}

	entryID, err := findPackCommand(appCtx, commandName)
	if err != nil {
		logger.Error("failed to find command", zap.Error(err))
		return err
	}

	if entryID != "" {
		if err := launchExecProcess(appCtx, logger, entryID, "", args); err != nil {
			logger.Error("exec launch failed", zap.Error(err))
			return err
		}
	}

	sig := <-sigChan
	logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	cancel()

	go func() {
		<-sigChan
		logger.Error("force exit")
		os.Exit(1)
	}()

	if !silentLogs {
		logger.Info("shutting down (press Ctrl+C again to force exit)")
	}

	exitCode := shutdown.Perform(ctx, loader, logger, silentLogs)
	if exitCode != 0 {
		_ = logger.Sync()
		os.Exit(exitCode)
	}

	return nil
}
