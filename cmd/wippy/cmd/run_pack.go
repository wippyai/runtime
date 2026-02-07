package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/api/payload"
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

// findPackCommand finds a command entry in the pack.
// If commandName is empty, returns the first available command (preferring "run").
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

// isHubModuleRef identifies inputs that should be treated as hub references
// instead of local files/paths.
func isHubModuleRef(s string) bool {
	if strings.HasSuffix(s, ".wapp") {
		return false
	}

	if _, err := os.Stat(s); err == nil {
		return false
	}

	return hubModulePattern.MatchString(s)
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
func runFromPackFile(_ *cobra.Command, packFile string, args []string) error {
	memLimit := initMemoryLimit()

	banner.Print(silentLogs)

	logger, err := createCommandLogger()
	if err != nil {
		return NewCreateLoggerError(err)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("loading pack file", zap.String("file", packFile), zap.String("memory_limit", formatBytes(memLimit)))

	ctx, loader, runLogger, embedReg, err := bootstrapPackRuntime(logger)
	if err != nil {
		return err
	}
	defer embedReg.Close()

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

	runLogger.Info("loaded entries from pack", zap.Int("count", len(packEntries)))

	return runPackEntries(ctx, loader, runLogger, packEntries, args)
}

// runFromPackFiles executes runtime from multiple already resolved .wapp files.
func runFromPackFiles(_ *cobra.Command, packFiles []string, args []string) error {
	memLimit := initMemoryLimit()

	banner.Print(silentLogs)

	logger, err := createCommandLogger()
	if err != nil {
		return NewCreateLoggerError(err)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("loading pack files", zap.Strings("files", packFiles), zap.String("memory_limit", formatBytes(memLimit)))

	ctx, loader, runLogger, embedReg, err := bootstrapPackRuntime(logger)
	if err != nil {
		return err
	}
	defer embedReg.Close()

	packEntries, err := entries.LoadEntriesFromPaths(ctx, packFiles, runLogger)
	if err != nil {
		runLogger.Error("failed to load entries from packs", zap.Error(err))
		return NewLoadEntriesError("pack files", err)
	}

	// Register .wapp pack readers with embed registry for fs.embed support.
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

	runLogger.Info("loaded entries from packs", zap.Int("count", len(packEntries)))

	return runPackEntries(ctx, loader, runLogger, packEntries, args)
}

// runPackEntries starts runtime, applies pack entries to registry, optionally
// launches a command from the loaded pack, and waits for shutdown.
func runPackEntries(
	ctx context.Context,
	loader *bootpkg.Loader,
	logger *zap.Logger,
	packEntries []registry.Entry,
	args []string,
) error {
	sigChan := setupSupervisorSignalChannel(ctx)
	defer signal.Stop(sigChan)

	appCtx, cancel := context.WithCancel(ctx)
	defer cancel()

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

	waitForShutdownSignal(sigChan, logger, cancel)

	exitCode := shutdown.Perform(ctx, loader, logger, silentLogs)
	if exitCode != 0 {
		_ = logger.Sync()
		os.Exit(exitCode)
	}

	return nil
}
