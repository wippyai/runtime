// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/cmd/internal/banner"
	"github.com/wippyai/runtime/cmd/internal/entries"
	"github.com/wippyai/runtime/cmd/internal/shutdown"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// hubModulePattern matches hub references like org/module[@version|@label].
var hubModulePattern = regexp.MustCompile(`^([a-z][a-z0-9-]*)/([a-z][a-z0-9-]*)(?:@(.+))?$`)
var hubIdentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// testCommandName is the reserved entrypoint name for test runners. `wippy run`
// never selects it; `wippy test` selects only it.
const testCommandName = "test"

// runMode selects which entrypoint a run-like command targets.
type runMode int

const (
	modeRun runMode = iota
	modeTest
)

type packCommand struct {
	name    string
	entryID string
	main    bool
}

// findPackCommand resolves the entrypoint to execute from the loaded registry.
// In modeRun it never selects the test entrypoint; in modeTest it selects only the
// test entrypoint. See selectPackCommand for the selection rules.
func findPackCommand(ctx context.Context, commandName string, mode runMode) (string, error) {
	reg := registry.GetRegistry(ctx)
	if reg == nil {
		return "", fmt.Errorf("registry not available")
	}

	allEntries, err := reg.GetAllEntries()
	if err != nil {
		return "", fmt.Errorf("failed to query registry for pack commands: %w", err)
	}

	var commands []packCommand

	for _, e := range allEntries {
		if !isProcessKind(e.Kind) {
			continue
		}

		cmdMeta := extractCommandMeta(e.Meta)
		if cmdMeta == nil {
			continue
		}

		commands = append(commands, packCommand{name: cmdMeta.Name, entryID: e.ID.String(), main: cmdMeta.Main})
	}

	return selectPackCommand(commands, commandName, mode)
}

// selectPackCommand picks the entrypoint to execute.
//
// modeTest selects the test entrypoint (name == testCommandName) and errors when
// none exists.
//
// modeRun never selects the test entrypoint. With an explicit name it matches a
// non-test command (and refuses "test"). With no name it auto-selects the single
// main entrypoint, or the only non-test entrypoint; it errors when several non-test
// entrypoints exist without a main, and returns "" (boot the app without executing
// an entrypoint) when there is no non-test entrypoint at all.
func selectPackCommand(commands []packCommand, commandName string, mode runMode) (string, error) {
	if mode == modeTest {
		for _, c := range commands {
			if c.name == testCommandName {
				return c.entryID, nil
			}
		}
		return "", fmt.Errorf("no test entrypoint found")
	}

	if commandName == testCommandName {
		return "", fmt.Errorf("the %q entrypoint runs with 'wippy test', not 'wippy run'", testCommandName)
	}

	nonTest := make([]packCommand, 0, len(commands))
	for _, c := range commands {
		if c.name == testCommandName {
			continue
		}
		nonTest = append(nonTest, c)
	}

	if commandName != "" {
		for _, c := range nonTest {
			if c.name == commandName {
				return c.entryID, nil
			}
		}
		return "", fmt.Errorf("command %q not found in pack", commandName)
	}

	if len(nonTest) == 0 {
		return "", nil
	}

	var mainCommands []string
	var mainEntryID string
	for _, c := range nonTest {
		if !c.main {
			continue
		}
		mainCommands = append(mainCommands, c.name)
		mainEntryID = c.entryID
	}

	switch len(mainCommands) {
	case 1:
		return mainEntryID, nil
	case 0:
		if len(nonTest) == 1 {
			return nonTest[0].entryID, nil
		}

		names := make([]string, len(nonTest))
		for i, c := range nonTest {
			names[i] = c.name
		}

		sort.Strings(names)
		return "", fmt.Errorf("no entrypoint specified; run one of: %s", strings.Join(names, ", "))
	default:
		return "", fmt.Errorf("multiple commands marked as main in pack: %s", strings.Join(mainCommands, ", "))
	}
}

// isHubModuleRef identifies inputs that should be treated as hub references
// instead of local files/paths.
func isHubModuleRef(s string) bool {
	if hasWappExtension(s) {
		return false
	}

	if _, err := os.Stat(s); err == nil {
		return false
	}

	return hubModulePattern.MatchString(s)
}

func hasWappExtension(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".wapp")
}

// downloadHubModule resolves dependency graph for a hub reference, downloads
// required packs into cache, updates lock metadata, and returns local pack paths.
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

	resolved, err := hub.Resolve(downloadCtx, client, []hub.DependencySpec{
		{Org: org, Name: module, Constraint: constraint},
	}, nil)
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

// updateLockFile persists resolved module version/hash into wippy.lock.
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

// isVersionString returns true for simple dotted version strings.
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

// getCacheDir returns the local cache directory used for downloaded packs.
func getCacheDir() string {
	if cacheDir := os.Getenv("WIPPY_CACHE_DIR"); cacheDir != "" {
		return cacheDir
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		return filepath.Join(homeDir, ".wippy", "cache")
	}

	return filepath.Join(os.TempDir(), "wippy-cache")
}

// runFromPackFile executes runtime from one .wapp file.
func runFromPackFile(cmd *cobra.Command, packFile string, args []string, mode runMode) error {
	memLimit := initMemoryLimit()

	banner.Print(silentLogs)

	logger, err := createCommandLogger()
	if err != nil {
		return NewCreateLoggerError(err)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("loading pack file", zap.String("file", packFile), zap.String("memory_limit", formatBytes(memLimit)))

	runtimeDefaults, err := loadPackRuntimeDefaults(packFile, logger)
	if err != nil {
		return fmt.Errorf("failed to load runtime defaults from pack metadata: %w", err)
	}
	if runtimeDefaults != nil {
		logger.Info("applied runtime defaults from pack metadata", zap.Int("setting_count", len(runtimeDefaults.Keys())))
	}

	ctx, loader, runLogger, embedReg, err := bootstrapPackRuntimeWithDefaults(cmd, logger, runtimeDefaults)
	if err != nil {
		return err
	}
	defer embedReg.Close()

	packEntries, err := loadPackEntries([]string{packFile}, embedReg)
	if err != nil {
		runLogger.Error("failed to load entries from pack", zap.Error(err))
		return NewLoadEntriesError(packFile, err)
	}

	runLogger.Info("loaded entries from pack", zap.Int("count", len(packEntries)))

	return runPackEntries(ctx, loader, runLogger, packEntries, args, mode)
}

// runFromPackFiles executes runtime from multiple already resolved .wapp files.
func runFromPackFiles(cmd *cobra.Command, packFiles []string, args []string, mode runMode) error {
	memLimit := initMemoryLimit()

	banner.Print(silentLogs)

	logger, err := createCommandLogger()
	if err != nil {
		return NewCreateLoggerError(err)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("loading pack files", zap.Strings("files", packFiles), zap.String("memory_limit", formatBytes(memLimit)))

	runtimeDefaults, err := loadPackRuntimeDefaultsFromFiles(packFiles, logger)
	if err != nil {
		return fmt.Errorf("failed to load runtime defaults from pack metadata: %w", err)
	}
	if runtimeDefaults != nil {
		logger.Info("applied runtime defaults from pack metadata", zap.Int("setting_count", len(runtimeDefaults.Keys())))
	}

	ctx, loader, runLogger, embedReg, err := bootstrapPackRuntimeWithDefaults(cmd, logger, runtimeDefaults)
	if err != nil {
		return err
	}
	defer embedReg.Close()

	packEntries, err := loadPackEntries(packFiles, embedReg)
	if err != nil {
		runLogger.Error("failed to load entries from packs", zap.Error(err))
		return NewLoadEntriesError("pack files", err)
	}

	runLogger.Info("loaded entries from packs", zap.Int("count", len(packEntries)))

	return runPackEntries(ctx, loader, runLogger, packEntries, args, mode)
}

// runPackEntries starts runtime, applies pack entries to registry, optionally
// launches a command from the loaded pack, and waits for shutdown.
func runPackEntries(
	ctx context.Context,
	loader *bootpkg.Loader,
	logger *zap.Logger,
	packEntries []registry.Entry,
	args []string,
	mode runMode,
) error {
	sigChan := setupSupervisorSignalChannel(ctx)
	defer signal.Stop(sigChan)

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := loader.Start(appCtx); err != nil {
		logger.Error("start failed", zap.Error(err))
		return NewStartComponentsError(err)
	}

	if err := applyPackEntries(appCtx, packEntries, logger); err != nil {
		return err
	}

	if !silentLogs {
		logger.Info("runtime ready")
	}

	commandName := ""
	if mode == modeRun && len(args) > 0 {
		commandName = args[0]
		args = args[1:]
	}

	entryID, err := findPackCommand(appCtx, commandName, mode)
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

	waitForShutdownSignal(sigChan, logger, cancel)

	exitCode := shutdown.Perform(ctx, loader, logger, silentLogs)
	if exitCode != 0 {
		_ = logger.Sync()
		os.Exit(exitCode)
	}

	return nil
}

// applyPackEntries restores packed entries as baseline state after applying the
// canonical entry normalization pipeline.
func applyPackEntries(ctx context.Context, packEntries []registry.Entry, logger *zap.Logger) error {
	if err := entries.NormalizeEntries(ctx, &packEntries); err != nil {
		return err
	}

	return entries.LoadEntriesToRegistry(ctx, packEntries, logger)
}

type packReaderRegistry interface {
	Register(packPath string, reader *wapp.Reader, file *os.File) error
}

func loadPackEntries(packFiles []string, embedReg packReaderRegistry) ([]registry.Entry, error) {
	packEntries := make([]registry.Entry, 0)

	for _, packFile := range packFiles {
		if !hasWappExtension(packFile) {
			return nil, fmt.Errorf("unsupported pack format %q", packFile)
		}

		file, err := os.Open(packFile)
		if err != nil {
			return nil, fmt.Errorf("open pack %s: %w", packFile, err)
		}

		packReader, err := entries.NewPackReader(file, nil)
		if err != nil {
			file.Close()
			return nil, fmt.Errorf("read pack %s: %w", packFile, err)
		}

		if err := embedReg.Register(packFile, packReader.Reader(), file); err != nil {
			file.Close()
			return nil, fmt.Errorf("register embed resources for %s: %w", packFile, err)
		}

		loadedEntries, err := packReader.GetEntries()
		if err != nil {
			return nil, fmt.Errorf("read entries from %s: %w", packFile, err)
		}

		moduleName, moduleVersion := moduleIdentityFromPackMetadata(packReader.Reader())
		if moduleName != "" {
			annotateEntriesModuleMeta(loadedEntries, moduleName, moduleVersion)
		}

		packEntries = append(packEntries, loadedEntries...)
	}

	return packEntries, nil
}

func moduleIdentityFromPackMetadata(reader *wapp.Reader) (moduleName string, moduleVersion string) {
	if reader == nil {
		return "", ""
	}

	metadata, err := reader.GetMetadata()
	if err != nil || len(metadata) == 0 {
		return "", ""
	}

	version, _ := metadata["version"].(string)
	namespace, _ := metadata["namespace"].(string)
	name, _ := metadata["name"].(string)

	if namespace == "" || name == "" {
		return "", version
	}

	suffix := "." + name
	if !strings.HasSuffix(namespace, suffix) {
		return "", version
	}

	org := strings.TrimSuffix(namespace, suffix)
	if !hubIdentPattern.MatchString(org) || !hubIdentPattern.MatchString(name) {
		return "", version
	}

	return org + "/" + name, version
}

func annotateEntriesModuleMeta(items []registry.Entry, moduleName string, moduleVersion string) {
	if moduleName == "" {
		return
	}

	for i := range items {
		meta := items[i].Meta
		if meta == nil {
			meta = attrs.NewBag()
		}

		if existingModule, _ := meta["module"].(string); existingModule == "" {
			meta["module"] = moduleName
		}
		if moduleVersion != "" {
			if existingVersion, _ := meta["module_version"].(string); existingVersion == "" {
				meta["module_version"] = moduleVersion
			}
		}

		items[i].Meta = meta
	}
}
