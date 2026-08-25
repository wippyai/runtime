// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/semver"
	bootpkg "github.com/wippyai/runtime/boot"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/graph"
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
func commandsFromEntries(items []registry.Entry) ([]packCommand, error) {
	var commands []packCommand
	for _, e := range items {
		if !isProcessKind(e.Kind) {
			continue
		}

		cmdMeta, err := extractCommandMeta(e.Meta)
		if err != nil {
			return nil, fmt.Errorf("decode command metadata for %s: %w", e.ID.String(), err)
		}
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

	return commands, nil
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

	return commandsFromEntries(allEntries)
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
		if !packCommandAllowed(entryModuleOwner(reg, entry.ID), mainModule) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return commandsFromEntries(filtered)
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

// entryModuleOwner reads ownership from registry provenance; author metadata
// named "module" is ordinary payload and never consulted.
func entryModuleOwner(reg registry.Registry, id registry.ID) string {
	reader, ok := reg.(interface {
		EntryProvenance(registry.ID) (registry.EntryProvenance, bool)
	})
	if !ok {
		return ""
	}
	p, _ := reader.EntryProvenance(id)
	return p.Module
}

func packCommandAllowed(module, mainModule string) bool {
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

// useLockedHubDeployment decides whether a Hub-looking run argument identifies
// an already established deployment. The lock file is the authority boundary:
// its absence permits bootstrap resolution, while its presence forbids an
// implicit graph change.
func useLockedHubDeployment(ref, lockPath string) (bool, error) {
	if _, err := os.Stat(lockPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect deployment lock %s: %w", lockPath, err)
	}

	requested, err := parseModuleRef(ref)
	if err != nil {
		return false, err
	}
	requestedName := requested.Org + "/" + requested.Module

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return false, fmt.Errorf("load deployment lock %s: %w", lockPath, err)
	}
	roots := lockObj.GetRootModules()
	if len(roots) != 1 {
		return false, fmt.Errorf("deployment lock %s must select exactly one root module; remove it to bootstrap a new deployment", lockPath)
	}
	root := roots[0]
	rootModule, _ := lockObj.GetModule(root)
	if requestedName != root {
		return false, fmt.Errorf("deployment lock %s selects %s@%s, not %s; use a fresh directory to bootstrap another application", lockPath, root, rootModule.Version, requestedName)
	}
	if requested.Version == "" {
		return true, nil
	}

	requestedVersion, err := semver.ParseVersion(requested.Version)
	if err != nil {
		return false, fmt.Errorf("deployment lock %s already pins %s@%s; selector @%s requires Hub resolution, so use 'wippy update' or omit the selector to restart", lockPath, root, rootModule.Version, requested.Version)
	}
	lockedVersion, err := semver.ParseVersion(rootModule.Version)
	if err != nil {
		return false, fmt.Errorf("deployment lock %s contains invalid root version %q for %s", lockPath, rootModule.Version, root)
	}
	if requestedVersion != lockedVersion {
		return false, fmt.Errorf("deployment lock %s pins %s@%s, not @%s; use 'wippy update' to change the deployment", lockPath, root, rootModule.Version, requested.Version)
	}

	return true, nil
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
			return nil, err
		}

		// A Hub reference is a deployment bootstrap, not a disposable cache run.
		// Materialize every verified pack under the lock's vendor directory so a
		// subsequent bare `wippy run` is fully local and uses the exact lock graph.
		packPath, err = materializeHubRunPack(packPath, moduleName, m.Version, m.Digest, m.SizeBytes)
		if err != nil {
			return nil, err
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

func materializeHubRunPack(sourcePath, moduleName, version, digest string, size uint64) (string, error) {
	lockObj, err := lock.New(defaultLockFile)
	if err != nil {
		return "", fmt.Errorf("load deployment lock: %w", err)
	}
	name, err := graph.ParseName(moduleName)
	if err != nil {
		return "", fmt.Errorf("invalid resolved module %q: %w", moduleName, err)
	}
	lockDir := filepath.Dir(lockObj.Path())
	vendorDir := lock.ResolveLockPath(lockDir, lockObj.GetVendorPath())
	destination := filepath.Join(vendorDir, lock.WappPath(name, version))
	if filepath.Clean(sourcePath) == filepath.Clean(destination) {
		return destination, nil
	}
	if err := hub.VerifyDownloadedArtifact(destination, digest, size); err == nil {
		return destination, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("verify installed %s@%s: %w", moduleName, version, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", fmt.Errorf("create deployment vendor directory: %w", err)
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("open cached %s@%s: %w", moduleName, version, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".hub-run-pack-*")
	if err != nil {
		_ = source.Close()
		return "", fmt.Errorf("stage %s@%s: %w", moduleName, version, err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = source.Close()
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, source); err != nil {
		return "", fmt.Errorf("copy %s@%s into deployment: %w", moduleName, version, err)
	}
	if err := source.Close(); err != nil {
		return "", fmt.Errorf("close cached %s@%s: %w", moduleName, version, err)
	}
	if err := tmp.Sync(); err != nil {
		return "", fmt.Errorf("sync staged %s@%s: %w", moduleName, version, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close staged %s@%s: %w", moduleName, version, err)
	}
	if err := hub.VerifyDownloadedArtifact(tmpPath, digest, size); err != nil {
		return "", fmt.Errorf("verify staged %s@%s: %w", moduleName, version, err)
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		return "", fmt.Errorf("install %s@%s: %w", moduleName, version, err)
	}
	committed = true
	return destination, nil
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

	packEntries, packProv, err := loadPackEntries([]string{packFile}, mainModule, embedReg)
	if err != nil {
		runLogger.Error("failed to load entries from pack", zap.Error(err))
		return NewLoadEntriesError(packFile, err)
	}

	runLogger.Info("loaded entries from pack", zap.Int("count", len(packEntries)))
	sourcePaths, err := packSourcePaths([]string{packFile}, mainModule)
	if err != nil {
		return err
	}
	entries.ConfigureSourceLoader(ctx, sourcePaths, runLogger)

	return runPackEntries(ctx, loader, runLogger, packEntries, packProv, args, useCase, mainModule)
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

	packEntries, packProv, err := loadPackEntries(packFiles, mainModule, embedReg)
	if err != nil {
		runLogger.Error("failed to load entries from packs", zap.Error(err))
		return NewLoadEntriesError("pack files", err)
	}

	runLogger.Info("loaded entries from packs", zap.Int("count", len(packEntries)))
	sourcePaths, err := packSourcePaths(packFiles, mainModule)
	if err != nil {
		return err
	}
	entries.ConfigureSourceLoader(ctx, sourcePaths, runLogger)

	return runPackEntries(ctx, loader, runLogger, packEntries, packProv, args, useCase, mainModule)
}

func packSourcePaths(packFiles []string, rootModule string) ([]lock.ModuleLoadPath, error) {
	paths := make([]lock.ModuleLoadPath, 0, len(packFiles))
	for _, packFile := range packFiles {
		module, version, err := moduleIdentityFromPackFile(packFile)
		if err != nil {
			return nil, fmt.Errorf("load source identity from %s: %w", packFile, err)
		}
		paths = append(paths, lock.ModuleLoadPath{
			Path:    packFile,
			Module:  module,
			Version: version,
			Root:    module != "" && module == rootModule,
		})
	}
	return paths, nil
}

// runPackEntries starts runtime, applies pack entries to registry, optionally
// launches a command from the loaded pack, and waits for shutdown.
func runPackEntries(
	ctx context.Context,
	loader *bootpkg.Loader,
	logger *zap.Logger,
	packEntries []registry.Entry,
	packProv registry.ProvMap,
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

	if err := applyPackEntries(appCtx, packEntries, packProv, logger); err != nil {
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
		execCtx, stopExecSignals := newExecSignalContext(appCtx)
		execErr := launchExecProcess(execCtx, logger, entryID, "", args)
		interrupted := execWasInterrupted(execCtx, appCtx, execErr)
		stopExecSignals()
		if execErr != nil && !interrupted {
			logger.Error("exec launch failed", zap.Error(execErr))
			return execErr
		}
		if interrupted {
			logger.Info("exec interrupted", zap.String("signal", "SIGINT"))
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
func applyPackEntries(ctx context.Context, packEntries []registry.Entry, prov registry.ProvMap, logger *zap.Logger) error {
	if err := entries.NormalizeEntries(ctx, &packEntries, prov); err != nil {
		return err
	}

	return entries.LoadEntriesToRegistry(ctx, packEntries, prov, logger)
}

type packReaderRegistry interface {
	Register(packPath string, reader *wapp.Reader, file *os.File) error
}

type modulePackReaderRegistry interface {
	RegisterPack(packPath, module, version string, reader *wapp.Reader, file *os.File) error
}

func loadPackEntries(packFiles []string, rootModule string, embedReg packReaderRegistry) ([]registry.Entry, registry.ProvMap, error) {
	packEntries := make([]registry.Entry, 0)
	prov := make(registry.ProvMap)

	for _, packFile := range packFiles {
		if !hasWappExtension(packFile) {
			return nil, nil, fmt.Errorf("unsupported pack format %q", packFile)
		}

		file, err := os.Open(packFile)
		if err != nil {
			return nil, nil, fmt.Errorf("open pack %s: %w", packFile, err)
		}

		packReader, err := entries.NewPackReader(file, nil)
		if err != nil {
			file.Close()
			return nil, nil, fmt.Errorf("read pack %s: %w", packFile, err)
		}

		moduleName, moduleVersion := moduleIdentityFromPackMetadata(packReader.Reader())
		packDigest := ""
		if moduleName != "" {
			var digestErr error
			packDigest, digestErr = packFileDigest(packFile)
			if digestErr != nil {
				file.Close()
				return nil, nil, fmt.Errorf("digest pack %s: %w", packFile, digestErr)
			}
		}
		if err := registerPackResources(embedReg, packFile, moduleName, moduleVersion, packReader.Reader(), file); err != nil {
			file.Close()
			return nil, nil, fmt.Errorf("register embed resources for %s: %w", packFile, err)
		}

		loadedEntries, err := packReader.GetEntries()
		if err != nil {
			return nil, nil, fmt.Errorf("read entries from %s: %w", packFile, err)
		}

		if moduleName != "" {
			for i := range loadedEntries {
				id := loadedEntries[i].ID.Canonical()
				prov[id] = registry.EntryProvenance{
					Module:  moduleName,
					Version: moduleVersion,
					Digest:  packDigest,
					Root:    moduleName == rootModule && loadedEntries[i].Kind == registry.NamespaceDependency,
				}
			}
		} else {
			packProv, provErr := packProvenanceFromMetadata(packReader.Reader(), loadedEntries)
			if provErr != nil {
				return nil, nil, fmt.Errorf("read provenance from %s: %w", packFile, provErr)
			}
			for id, pr := range packProv {
				prov[id] = pr
			}
			if err := registerMonolithicPackResourceAliases(embedReg, packFile, packReader.Reader(), loadedEntries, packProv); err != nil {
				return nil, nil, fmt.Errorf("register embed resource aliases for %s: %w", packFile, err)
			}
		}

		packEntries = append(packEntries, loadedEntries...)
	}

	// The map is total: entries from packs that predate the provenance frame
	// carry the explicit host record.
	for _, entry := range packEntries {
		if _, ok := prov[entry.ID.Canonical()]; !ok {
			prov[entry.ID.Canonical()] = registry.EntryProvenance{}
		}
	}

	return packEntries, prov, nil
}

func registerPackResources(embedReg packReaderRegistry, packFile, moduleName, moduleVersion string, reader *wapp.Reader, file *os.File) error {
	if moduleName != "" {
		if moduleReg, ok := embedReg.(modulePackReaderRegistry); ok {
			return moduleReg.RegisterPack(packFile, moduleName, moduleVersion, reader, file)
		}
	}
	return embedReg.Register(packFile, reader, file)
}

func registerMonolithicPackResourceAliases(embedReg packReaderRegistry, packFile string, reader *wapp.Reader, loadedEntries []registry.Entry, prov registry.ProvMap) error {
	moduleReg, ok := embedReg.(modulePackReaderRegistry)
	if !ok {
		return nil
	}

	seen := make(map[string]struct{})
	for _, entry := range loadedEntries {
		p, ok := prov[entry.ID.Canonical()]
		if !ok || p.Module == "" {
			continue
		}

		key := p.Module + "\x00" + p.Version
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		if err := moduleReg.RegisterPack(monolithicPackAliasPath(packFile, p.Module, p.Version), p.Module, p.Version, reader, nil); err != nil {
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
