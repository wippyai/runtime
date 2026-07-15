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

type hubPackDownloader interface {
	DownloadToFile(ctx context.Context, url, destPath string) error
}

// hubModulePattern matches hub references like org/module[@version|@label].
var hubModulePattern = regexp.MustCompile(`^([a-z][a-z0-9-]*)/([a-z][a-z0-9-]*)(?:@(.+))?$`)
var hubIdentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// defaultUseCase is the use case targeted by `wippy run`. An entry whose
// command meta omits a use case belongs to it. `wippy test` targets the "test"
// use case instead; future run-like commands target their own use case.
const defaultUseCase = "run"

type packCommand struct {
	name    string
	entryID string
	useCase string
	main    bool
}

// commandsFromEntries projects registry entries into the command entrypoints
// they declare, ignoring entries without process kind or command meta.
func commandsFromEntries(items []registry.Entry) []packCommand {
	var commands []packCommand
	for _, e := range items {
		if !isProcessKind(e.Kind) {
			continue
		}

		cmdMeta := extractCommandMeta(e.Meta)
		if cmdMeta == nil {
			continue
		}

		commands = append(commands, packCommand{
			name:    cmdMeta.Name,
			entryID: e.ID.String(),
			useCase: cmdMeta.UseCase,
			main:    cmdMeta.Main,
		})
	}

	return commands
}

// collectCommands gathers every command entrypoint declared in the loaded registry.
func collectCommands(ctx context.Context) ([]packCommand, error) {
	reg := registry.GetRegistry(ctx)
	if reg == nil {
		return nil, fmt.Errorf("registry not available")
	}

	allEntries, err := reg.GetAllEntries()
	if err != nil {
		return nil, fmt.Errorf("failed to query registry for commands: %w", err)
	}

	return commandsFromEntries(allEntries), nil
}

func collectPackCommands(ctx context.Context, mainModule string) ([]packCommand, error) {
	reg := registry.GetRegistry(ctx)
	if reg == nil {
		return nil, fmt.Errorf("registry not available")
	}

	allEntries, err := reg.GetAllEntries()
	if err != nil {
		return nil, fmt.Errorf("failed to query registry for commands: %w", err)
	}

	filtered := allEntries[:0]
	for _, entry := range allEntries {
		if !packCommandAllowed(entry.Meta, mainModule) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return commandsFromEntries(filtered), nil
}

// commandForUseCase maps a declared use case to the top-level CLI command that
// targets it (the default use case maps to `wippy run`, every other use case
// maps to a command of the same name, e.g. `wippy test`).
func commandForUseCase(useCase string) string {
	if useCase == defaultUseCase {
		return "run"
	}

	return useCase
}

func findPackCommandForModule(ctx context.Context, commandName, useCase, mainModule string) (string, error) {
	commands, err := collectPackCommands(ctx, mainModule)
	if err != nil {
		return "", err
	}

	return selectEntrypoint(commands, commandName, useCase)
}

func moduleMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	module, _ := meta["module"].(string)
	return module
}

func packCommandAllowed(meta map[string]any, mainModule string) bool {
	module := moduleMeta(meta)
	if mainModule == "" {
		return module == ""
	}
	return module == "" || module == mainModule
}

// selectEntrypoint picks the entrypoint to execute for a use case.
//
// Only entries whose declared use case matches are eligible. With an explicit
// name it returns the matching eligible entry; a name that exists only under a
// different use case yields a hint to invoke that use case's command. With no
// name it auto-selects the single main entrypoint, then the only eligible
// entrypoint, and errors when several remain without a main. When nothing
// matches it returns "" for the default use case (boot the app without executing
// an entrypoint) and errors for any other use case.
func selectEntrypoint(commands []packCommand, commandName, useCase string) (string, error) {
	matching := make([]packCommand, 0, len(commands))
	for _, c := range commands {
		if c.useCase == useCase {
			matching = append(matching, c)
		}
	}

	if commandName != "" {
		for _, c := range matching {
			if c.name == commandName {
				return c.entryID, nil
			}
		}

		for _, c := range commands {
			if c.name == commandName {
				return "", fmt.Errorf("the %q entrypoint belongs to the %q use case; run it with 'wippy %s'", commandName, c.useCase, commandForUseCase(c.useCase))
			}
		}

		return "", fmt.Errorf("command %q not found; use 'wippy run list' to see available commands", commandName)
	}

	if len(matching) == 0 {
		if useCase == defaultUseCase {
			return "", nil
		}
		return "", fmt.Errorf("no %s entrypoint found", useCase)
	}

	var mainCommands []string
	var mainEntryID string
	for _, c := range matching {
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
		if len(matching) == 1 {
			return matching[0].entryID, nil
		}

		names := make([]string, len(matching))
		for i, c := range matching {
			names[i] = c.name
		}

		sort.Strings(names)
		return "", fmt.Errorf("no entrypoint specified; run one of: %s", strings.Join(names, ", "))
	default:
		return "", fmt.Errorf("multiple commands marked as main: %s", strings.Join(mainCommands, ", "))
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

		if err := ensureHubPackCached(downloadCtx, client, m, packPath, moduleName, registryURL); err != nil {
			return nil, err
		}

		isRoot := m.Org == org && m.Name == module
		if err := updateLockFile(moduleName, m.Version, m.Digest, isRoot); err != nil {
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

func ensureHubPackCached(ctx context.Context, client hubPackDownloader, m hub.ResolvedModule, packPath, moduleName, registryURL string) error {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if _, err := os.Stat(packPath); err == nil {
		if err := hub.VerifyDownloadedArtifact(packPath, m.Digest, m.SizeBytes); err == nil {
			fmt.Printf("%s %s@%s (cached)\n", dimStyle.Render(""), moduleName, m.Version)
			return nil
		}
		fmt.Printf("%s Cached %s@%s failed integrity check; redownloading...\n", dimStyle.Render(""), moduleName, m.Version)
		if err := os.Remove(packPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove invalid cached pack %s: %w", packPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat cached pack %s: %w", packPath, err)
	}

	fmt.Printf("%s Downloading %s@%s...\n", dimStyle.Render(""), moduleName, m.Version)
	if m.URL == "" {
		return fmt.Errorf("no download URL for %s@%s from %s", moduleName, m.Version, registryURL)
	}
	if err := client.DownloadToFile(ctx, m.URL, packPath); err != nil {
		return fmt.Errorf("failed to download %s@%s from %s to %s: %w", moduleName, m.Version, registryURL, packPath, err)
	}
	if err := hub.VerifyDownloadedArtifact(packPath, m.Digest, m.SizeBytes); err != nil {
		_ = os.Remove(packPath)
		return fmt.Errorf("failed to verify downloaded %s@%s from %s: %w", moduleName, m.Version, registryURL, err)
	}
	return nil
}

// updateLockFile persists resolved module version/hash into wippy.lock.
func updateLockFile(moduleName, version, digest string, root bool) error {
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
	if root {
		lockObj.SetRootModule(moduleName)
	}
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
func runFromPackFile(cmd *cobra.Command, packFile string, args []string, useCase string) error {
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

	mainModule, _, err := moduleIdentityFromPackFile(packFile)
	if err != nil {
		return fmt.Errorf("failed to load main module identity from pack metadata: %w", err)
	}

	packEntries, err := loadPackEntries([]string{packFile}, mainModule, embedReg)
	if err != nil {
		runLogger.Error("failed to load entries from pack", zap.Error(err))
		return NewLoadEntriesError(packFile, err)
	}

	runLogger.Info("loaded entries from pack", zap.Int("count", len(packEntries)))

	return runPackEntries(ctx, loader, runLogger, packEntries, args, useCase, mainModule)
}

// runFromPackFiles executes runtime from multiple already resolved .wapp files.
func runFromPackFiles(cmd *cobra.Command, packFiles []string, args []string, useCase string) error {
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

	mainModule := ""
	if len(packFiles) > 0 {
		var err error
		mainModule, _, err = moduleIdentityFromPackFile(packFiles[len(packFiles)-1])
		if err != nil {
			return fmt.Errorf("failed to load main module identity from pack metadata: %w", err)
		}
	}

	packEntries, err := loadPackEntries(packFiles, mainModule, embedReg)
	if err != nil {
		runLogger.Error("failed to load entries from packs", zap.Error(err))
		return NewLoadEntriesError("pack files", err)
	}

	runLogger.Info("loaded entries from packs", zap.Int("count", len(packEntries)))

	return runPackEntries(ctx, loader, runLogger, packEntries, args, useCase, mainModule)
}

// runPackEntries starts runtime, applies pack entries to registry, optionally
// launches a command from the loaded pack, and waits for shutdown.
func runPackEntries(
	ctx context.Context,
	loader *bootpkg.Loader,
	logger *zap.Logger,
	packEntries []registry.Entry,
	args []string,
	useCase string,
	mainModule string,
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
	if useCase == defaultUseCase && len(args) > 0 {
		commandName = args[0]
		args = args[1:]
	}

	entryID, err := findPackCommandForModule(appCtx, commandName, useCase, mainModule)
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

	waitForShutdownSignal(sigChan, logger, nil)

	exitCode := shutdown.Perform(ctx, loader, logger, silentLogs)
	if exitCode != 0 {
		_ = logger.Sync()
		os.Exit(exitCode)
	}

	return nil
}

func moduleIdentityFromPackFile(packFile string) (moduleName string, moduleVersion string, err error) {
	file, err := os.Open(packFile)
	if err != nil {
		return "", "", fmt.Errorf("open pack %s: %w", packFile, err)
	}
	defer file.Close()

	reader, err := entries.NewPackReader(file, nil)
	if err != nil {
		return "", "", fmt.Errorf("read pack %s: %w", packFile, err)
	}

	moduleName, moduleVersion = moduleIdentityFromPackMetadata(reader.Reader())
	return moduleName, moduleVersion, nil
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

type modulePackReaderRegistry interface {
	RegisterPack(packPath, module, version string, reader *wapp.Reader, file *os.File) error
}

func loadPackEntries(packFiles []string, rootModule string, embedReg packReaderRegistry) ([]registry.Entry, error) {
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

		moduleName, moduleVersion := moduleIdentityFromPackMetadata(packReader.Reader())
		if err := registerPackResources(embedReg, packFile, moduleName, moduleVersion, packReader.Reader(), file); err != nil {
			file.Close()
			return nil, fmt.Errorf("register embed resources for %s: %w", packFile, err)
		}

		loadedEntries, err := packReader.GetEntries()
		if err != nil {
			return nil, fmt.Errorf("read entries from %s: %w", packFile, err)
		}

		if moduleName != "" {
			annotateEntriesModuleMeta(loadedEntries, moduleName, moduleVersion, moduleName == rootModule)
		} else {
			if err := registerMonolithicPackResourceAliases(embedReg, packFile, packReader.Reader(), loadedEntries); err != nil {
				return nil, fmt.Errorf("register embed resource aliases for %s: %w", packFile, err)
			}
		}

		packEntries = append(packEntries, loadedEntries...)
	}

	return packEntries, nil
}

func registerPackResources(embedReg packReaderRegistry, packFile, moduleName, moduleVersion string, reader *wapp.Reader, file *os.File) error {
	if moduleName != "" {
		if moduleReg, ok := embedReg.(modulePackReaderRegistry); ok {
			return moduleReg.RegisterPack(packFile, moduleName, moduleVersion, reader, file)
		}
	}
	return embedReg.Register(packFile, reader, file)
}

func registerMonolithicPackResourceAliases(embedReg packReaderRegistry, packFile string, reader *wapp.Reader, loadedEntries []registry.Entry) error {
	moduleReg, ok := embedReg.(modulePackReaderRegistry)
	if !ok {
		return nil
	}

	seen := make(map[string]struct{})
	for _, entry := range loadedEntries {
		if entry.Meta == nil {
			continue
		}

		moduleName, _ := entry.Meta["module"].(string)
		if moduleName == "" {
			continue
		}

		moduleVersion, _ := entry.Meta["module_version"].(string)
		key := moduleName + "\x00" + moduleVersion
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		if err := moduleReg.RegisterPack(monolithicPackAliasPath(packFile, moduleName, moduleVersion), moduleName, moduleVersion, reader, nil); err != nil {
			return err
		}
	}

	return nil
}

func monolithicPackAliasPath(packFile, moduleName, moduleVersion string) string {
	if moduleVersion == "" {
		return packFile + "#" + moduleName
	}
	return packFile + "#" + moduleName + "@" + moduleVersion
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

func annotateEntriesModuleMeta(items []registry.Entry, moduleName string, moduleVersion string, root bool) {
	if moduleName == "" {
		return
	}

	for i := range items {
		if root && items[i].Kind == registry.NamespaceDependency {
			items[i].DependencyRoot = true
		}
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
