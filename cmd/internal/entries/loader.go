// SPDX-License-Identifier: MPL-2.0

// Package entries handles loading registry entries from lock files and managing module installation.
package entries

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/cmd/internal/hubclient"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	regtop "github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// LoadFromLockFile loads application entries from the lock file and applies them to the registry.
// This function executes between the Load and Start phases of the boot process.
// If modules are missing, it auto-installs them before loading entries.
func LoadFromLockFile(ctx context.Context, logger *zap.Logger) error {
	lockFilePath := lock.DefaultFilename

	lockPath, err := lock.Find(".", lockFilePath)
	if err != nil {
		logger.Info("no lock file found, starting with empty registry")
		return nil
	}

	logger.Info("loading entries from lock file", zap.String("path", lockPath))

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockPath, err))
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}

	if err := ensureModulesInstalledFromLock(ctx, lockObj, logger); err != nil {
		return NewEnsureModulesInstalledError(err)
	}

	modulePaths := lockObj.GetModuleLoadPaths()
	registerModuleSourceRoots(ctx, modulePaths)
	flatPaths := make([]string, len(modulePaths))
	for i, mp := range modulePaths {
		flatPaths[i] = mp.Path
	}
	logger.Debug("load paths from lock file", zap.Strings("paths", flatPaths))

	entries, err := loadEntriesWithModuleMeta(ctx, modulePaths, logger)
	if err != nil {
		return NewLoadEntriesFromPathsError(err)
	}

	logger.Info("loaded entries", zap.Int("count", len(entries)))

	// Register .wapp files with embed registry for fs.embed support
	if err := registerWappWithEmbedRegistry(ctx, flatPaths, logger); err != nil {
		return err
	}

	if err := LoadEntriesToRegistry(ctx, entries, logger); err != nil {
		return err // Already has context from registry
	}

	logger.Info("entries loaded to registry successfully")
	return nil
}

// EnsureModulesInstalled checks if modules from the lock file are installed,
// and auto-installs them if missing using the hub client.
func EnsureModulesInstalled(ctx context.Context, lockPath string, logger *zap.Logger) error {
	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(fmt.Errorf("lock file %s: %w", lockPath, err))
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(fmt.Errorf("lock file %s: %w", lockObj.Path(), err))
	}

	return ensureModulesInstalledFromLock(ctx, lockObj, logger)
}

func ensureModulesInstalledFromLock(ctx context.Context, lockObj *lock.Lock, logger *zap.Logger) error {
	modules := lockObj.GetModules()
	if len(modules) == 0 {
		return nil
	}

	lockDir := filepath.Dir(lockObj.Path())
	vendorPath := lock.ResolveLockPath(lockDir, lockObj.GetVendorPath())
	shouldUnpack := lockObj.ShouldUnpackModules()
	logger.Debug("checking modules installation",
		zap.String("vendor_path", vendorPath),
		zap.Bool("unpack_modules", shouldUnpack))

	// Check which modules need installation
	var missingModules []lock.Module
	for _, mod := range modules {
		if repl, ok := lockObj.GetReplacement(mod.Name); ok {
			logger.Debug("module is replaced by local source; skipping auto-install",
				zap.String("module", mod.Name),
				zap.String("replacement", repl.To))
			continue
		}

		name, err := graph.ParseName(mod.Name)
		if err != nil {
			logger.Warn("failed to parse module name", zap.String("module", mod.Name), zap.Error(err))
			continue
		}

		resolved := lock.ResolveModuleDir(vendorPath, name, mod.Version)
		if resolved.IsWapp {
			if shouldUnpack {
				// Migrate legacy .wapp to extracted directory when unpack is enabled
				dirPath := filepath.Join(vendorPath, lock.ModulePath(name))
				logger.Info("unpacking .wapp to directory", zap.String("module", mod.Name))
				if err := ExtractWappToDir(resolved.Path, dirPath); err != nil {
					return NewExtractModuleError(mod.Name, err)
				}
			}
			// When unpack=false, keep .wapp as-is (already installed)
			continue
		}

		if _, err := os.Stat(resolved.Path); err == nil {
			continue
		}

		missingModules = append(missingModules, mod)
	}

	if len(missingModules) == 0 {
		logger.Debug("all modules already installed")
		return nil
	}

	logger.Info("auto-installing missing modules", zap.Int("count", len(missingModules)))

	// Create hub client
	hubClient, err := createHubClient()
	if err != nil {
		return fmt.Errorf("failed to create hub client: %w", err)
	}

	for _, mod := range missingModules {
		name, err := graph.ParseName(mod.Name)
		if err != nil {
			logger.Warn("failed to parse module name", zap.String("module", mod.Name), zap.Error(err))
			continue
		}

		logger.Info("downloading module",
			zap.String("module", mod.Name),
			zap.String("version", mod.Version))

		moduleRef := mod.Name
		if mod.Version != "" {
			moduleRef = moduleRef + "@" + mod.Version
		}

		// Get download URL from hub
		downloadInfo, err := hubClient.GetDownloadURL(ctx, &hub.DownloadParams{
			Org:     name.Organization,
			Module:  name.Module,
			Version: mod.Version,
		})
		if err != nil {
			return NewDownloadModuleError(moduleRef, err)
		}

		if downloadInfo.URL == "" {
			return NewNoContentDownloadedError(moduleRef)
		}

		// Download .wapp file
		wappPath := lock.WappPath(name, mod.Version)
		fullWappPath := filepath.Join(vendorPath, wappPath)

		if err := hubClient.DownloadToFile(ctx, downloadInfo.URL, fullWappPath); err != nil {
			return NewDownloadModuleError(moduleRef, err)
		}

		if shouldUnpack {
			// Extract .wapp to source directory and remove the .wapp file
			dirPath := filepath.Join(vendorPath, lock.ModulePath(name))
			if err := os.RemoveAll(dirPath); err != nil {
				return NewExtractModuleError(moduleRef, err)
			}
			if err := ExtractWappToDir(fullWappPath, dirPath); err != nil {
				return NewExtractModuleError(moduleRef, err)
			}
		}
		// When unpack=false, keep .wapp file as-is
	}

	logger.Info("modules installed successfully")
	return nil
}

// createHubClient creates a hub client using stored credentials.
func createHubClient() (*hub.Client, error) {
	return hubclient.NewDefault()
}

// LoadEntriesFromPaths loads registry entries from the specified paths.
// Supports both directories (loaded via LoadFS) and .wapp files (loaded via PackReader).
func LoadEntriesFromPaths(ctx context.Context, paths []string, logger *zap.Logger) ([]regapi.Entry, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, ErrTranscoderNotFound
	}

	ldr := boot.GetLoader(ctx)
	if ldr == nil {
		return nil, ErrLoaderNotFound
	}

	var entries []regapi.Entry

	for _, path := range paths {
		var loadedEntries []regapi.Entry

		if filepath.Ext(path) == ".wapp" {
			// Wapp file: load via PackReader
			var err error
			loadedEntries, err = loadEntriesFromWapp(path, dtt)
			if err != nil {
				return nil, NewLoadFromPathError(path, err)
			}
		} else {
			stat, err := os.Stat(path)
			if os.IsNotExist(err) {
				logger.Warn("path not found, skipping", zap.String("path", path))
				continue
			}
			if err != nil {
				return nil, NewLoadFromPathError(path, err)
			}

			if stat.IsDir() {
				// Directory: load via FS
				dirFS := os.DirFS(path)
				loadedEntries, err = ldr.LoadFS(ctx, dirFS)
				if err != nil {
					return nil, NewLoadFromPathError(path, err)
				}
			} else {
				logger.Warn("unknown path type, skipping", zap.String("path", path))
				continue
			}
		}

		entries = append(entries, loadedEntries...)
	}

	if err := NormalizeEntries(ctx, &entries); err != nil {
		return nil, err
	}

	return entries, nil
}

// LoadEntriesFromModuleLoadPaths loads entries from lock module paths with module metadata.
// Module-owned entries are tagged with meta.module/meta.module_version before pipeline stages.
func LoadEntriesFromModuleLoadPaths(
	ctx context.Context,
	modulePaths []lock.ModuleLoadPath,
	logger *zap.Logger,
) ([]regapi.Entry, error) {
	registerModuleSourceRoots(ctx, modulePaths)
	return loadEntriesWithModuleMeta(ctx, modulePaths, logger)
}

func registerModuleSourceRoots(ctx context.Context, modulePaths []lock.ModuleLoadPath) {
	roots := moduleapi.SourceRoots{}
	for _, mp := range modulePaths {
		if mp.Module == "" || filepath.Ext(mp.Path) == ".wapp" {
			continue
		}

		rootPath := mp.SourceRoot
		if rootPath == "" {
			rootPath = mp.Path
		}

		stat, err := os.Stat(rootPath)
		if err != nil || !stat.IsDir() {
			continue
		}

		root, err := filepath.Abs(rootPath)
		if err != nil {
			continue
		}
		roots[mp.Module] = root
	}

	moduleapi.WithSourceRoots(ctx, roots)
}

// loadEntriesWithModuleMeta loads entries from annotated paths and tags module entries
// with their owning module name and version. App source entries remain untagged.
func loadEntriesWithModuleMeta(ctx context.Context, modulePaths []lock.ModuleLoadPath, logger *zap.Logger) ([]regapi.Entry, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, ErrTranscoderNotFound
	}

	ldr := boot.GetLoader(ctx)
	if ldr == nil {
		return nil, ErrLoaderNotFound
	}

	var entries []regapi.Entry

	for _, mp := range modulePaths {
		loaded, err := loadEntriesFromPath(ctx, mp.Path, ldr, dtt, logger)
		if err != nil {
			return nil, err
		}

		if shouldApplyModuleConfigFilters(mp) {
			loaded, err = applyModuleConfigFilters(ctx, mp, loaded, logger)
			if err != nil {
				return nil, err
			}
		}

		if mp.Module != "" {
			for i := range loaded {
				loaded[i] = markModuleMeta(loaded[i], mp.Module, mp.Version)
			}
		}

		entries = append(entries, loaded...)
	}

	if err := NormalizeEntries(ctx, &entries); err != nil {
		return nil, err
	}

	return entries, nil
}

func shouldApplyModuleConfigFilters(mp lock.ModuleLoadPath) bool {
	// Apply the module's wippy.yaml exclude/exclude_meta rules whenever we're
	// loading a module from a directory tree — both versioned vendored sources
	// and replacement paths (wippy.lock `replacements:`). Without this, a host
	// app that points `replacements:` at a module's source picks up that
	// module's own test fixtures (e.g. test/_index.yaml under namespace `app`),
	// which then collide with the host's real entries via "use last definition"
	// dedup. .wapp files are skipped: they were filtered at publish time, and
	// re-running the filter would require parsing a manifest the archive does
	// not expose.
	return mp.Module != "" && filepath.Ext(mp.Path) != ".wapp"
}

func applyModuleConfigFilters(
	ctx context.Context,
	mp lock.ModuleLoadPath,
	entries []regapi.Entry,
	logger *zap.Logger,
) ([]regapi.Entry, error) {
	cfg, err := depconfig.Load(mp.Path)
	if err != nil {
		if logger != nil {
			logger.Debug("module config not loaded for source dependency",
				zap.String("module", mp.Module),
				zap.String("path", mp.Path),
				zap.Error(err))
		}
		return entries, nil
	}
	if len(cfg.Exclude) == 0 && len(cfg.ExcludeMeta) == 0 {
		return entries, nil
	}

	filtered := append([]regapi.Entry(nil), entries...)
	stage := stages.DisableWithOptions(stages.DisableOptions{
		Entries:     cfg.Exclude,
		MetaFilters: cfg.ExcludeMeta,
	})
	if err := stage.Execute(ctx, &filtered); err != nil {
		return nil, err
	}

	if logger != nil && len(filtered) != len(entries) {
		logger.Debug("applied module config filters",
			zap.String("module", mp.Module),
			zap.String("version", mp.Version),
			zap.String("path", mp.Path),
			zap.Int("before", len(entries)),
			zap.Int("after", len(filtered)))
	}
	return filtered, nil
}

// NormalizeEntries applies the canonical entry normalization pipeline.
//
// Order is intentional:
//  1. pre-link override: allows overriding dependency/requirement inputs
//  2. disable: removes excluded entries before link mutation
//  3. link: resolves requirements from dependencies/defaults
//  4. post-link override: allows explicit final-value overrides
func NormalizeEntries(ctx context.Context, entries *[]regapi.Entry) error {
	pipeline := build.New(
		stages.Override(),
		stages.Disable(),
		stages.Link(),
		stages.Override(),
	)

	if err := pipeline.Execute(ctx, entries); err != nil {
		return NewExecutePipelineError(err)
	}

	return nil
}

// loadEntriesFromPath loads entries from a single path (directory or .wapp file).
func loadEntriesFromPath(ctx context.Context, path string, ldr boot.Loader, dtt payload.Transcoder, logger *zap.Logger) ([]regapi.Entry, error) {
	if filepath.Ext(path) == ".wapp" {
		return loadEntriesFromWapp(path, dtt)
	}

	stat, err := os.Stat(path)
	if os.IsNotExist(err) {
		logger.Warn("path not found, skipping", zap.String("path", path))
		return nil, nil
	}
	if err != nil {
		return nil, NewLoadFromPathError(path, err)
	}

	if stat.IsDir() {
		dirFS := os.DirFS(path)
		loaded, err := ldr.LoadFS(ctx, dirFS)
		if err != nil {
			return nil, NewLoadFromPathError(path, err)
		}
		return loaded, nil
	}

	logger.Warn("unknown path type, skipping", zap.String("path", path))
	return nil, nil
}

func markModuleMeta(entry regapi.Entry, moduleName, moduleVersion string) regapi.Entry {
	meta := entry.Meta
	if meta == nil {
		meta = attrs.NewBag()
	} else {
		meta = attrs.NewBagFrom(meta)
	}
	meta.Set("module", moduleName)
	if moduleVersion != "" {
		meta.Set("module_version", moduleVersion)
	}
	entry.Meta = meta
	return entry
}

// loadEntriesFromWapp loads entries from a .wapp file.
func loadEntriesFromWapp(path string, dtt payload.Transcoder) ([]regapi.Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader, err := NewPackReader(file, dtt)
	if err != nil {
		return nil, err
	}

	return reader.GetEntries()
}

// LoadEntriesToRegistry loads entries into the registry using LoadState to restore from history.
func LoadEntriesToRegistry(ctx context.Context, entries []regapi.Entry, logger *zap.Logger) error {
	if err := waitForListenerReadiness(ctx, logger); err != nil {
		return err
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		return ErrRegistryNotFound
	}

	resolver := regapi.GetResolver(ctx)
	if resolver == nil {
		return ErrResolverNotFound
	}

	// Check for duplicate entry IDs
	entryByID := make(map[string]int)
	for _, entry := range entries {
		entryByID[entry.ID.String()]++
	}

	duplicateCount := 0
	duplicateIDs := make([]string, 0)
	for id, count := range entryByID {
		if count > 1 {
			duplicateCount += count - 1
			duplicateIDs = append(duplicateIDs, fmt.Sprintf("%s (x%d)", id, count))
		}
	}

	if duplicateCount > 0 {
		logger.Warn("duplicate entries detected (will use last definition)",
			zap.Int("duplicates", duplicateCount),
			zap.Strings("affected", duplicateIDs))
	}

	logger.Info("creating baseline state from entries", zap.Int("entry_count", len(entries)))

	baselineState := make(regapi.State, 0, len(entries))
	for _, entry := range entries {
		baselineState = append(baselineState, entry)
	}

	logger.Info("baseline state created", zap.Int("entry_count", len(baselineState)))

	hist := reg.History()
	head, err := hist.Head()
	switch {
	case err != nil:
		logger.Info("no history found, initializing registry with baseline state at v0")
		currentVer, err := reg.Current()
		if err != nil {
			return NewGetCurrentVersionError(err)
		}
		head = currentVer
	case head.ID() > 0:
		logger.Info("restoring registry state from history", zap.Uint("version", head.ID()))
	default:
		logger.Info("initializing registry with baseline state at v0")
	}

	if err := reg.LoadState(ctx, baselineState, head); err != nil {
		return err // Already wrapped by registry with proper context
	}

	logger.Debug("registry state loaded", zap.Uint("version", head.ID()))
	return nil
}

// PackReader wraps wapp.Reader for reading pack files.
type PackReader struct {
	reader *wapp.Reader
}

// NewPackReader creates a new pack reader from an io.ReaderAt.
// The transcoder parameter is kept for API compatibility but not used with wapp.
func NewPackReader(r io.ReaderAt, _ payload.Transcoder) (*PackReader, error) {
	reader, err := wapp.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &PackReader{reader: reader}, nil
}

// Reader returns the underlying wapp.Reader.
func (pr *PackReader) Reader() *wapp.Reader {
	return pr.reader
}

// GetEntries returns the entries from the pack file.
func (pr *PackReader) GetEntries() ([]regapi.Entry, error) {
	wappEntries, err := pr.reader.GetEntries()
	if err != nil {
		return nil, err
	}

	entries := make([]regapi.Entry, len(wappEntries))
	for i, we := range wappEntries {
		entries[i] = regapi.Entry{
			ID:   regapi.NewID(we.ID.Namespace, we.ID.Name),
			Kind: we.Kind,
			Meta: attrs.NewBagFrom(we.Meta),
			Data: payload.New(unwrapPayloadData(we.Data)),
		}
	}
	return entries, nil
}

// unwrapPayloadData extracts the inner data if the value is a serialized payload structure.
// This handles backward compatibility with wapp files that stored the full payload wrapper.
func unwrapPayloadData(data any) any {
	m, ok := data.(map[string]any)
	if !ok {
		return data
	}

	innerData, hasData := m["Data"]
	_, hasFormat := m["Format"]

	if hasData && hasFormat && len(m) == 2 {
		return innerData
	}

	return data
}

// ApplyToRegistry applies entries to the registry using topology-sorted change set.
func ApplyToRegistry(ctx context.Context, entries []regapi.Entry, resolver regapi.DependencyResolver, reg regapi.Registry, logger *zap.Logger) error {
	if err := waitForListenerReadiness(ctx, logger); err != nil {
		return err
	}

	logger.Debug("building change set from entries")
	changeSet, err := regtop.CreateChangeSetFromEntries(entries, resolver)
	if err != nil {
		return NewBuildChangeSetError(err)
	}
	logger.Debug("change set built")

	logger.Info("applying change set to registry", zap.Int("entry_count", len(entries)))
	version, err := reg.Apply(ctx, changeSet)
	if err != nil {
		logger.Error("apply failed", zap.Error(err))
		return NewApplyEntriesError(err)
	}

	logger.Info("entries applied to registry", zap.Any("version", version))
	return nil
}

func waitForListenerReadiness(ctx context.Context, logger *zap.Logger) error {
	readiness := bootpkg.GetReadiness(ctx)
	if readiness == nil {
		return nil
	}

	pending := readiness.Pending()
	if pending == 0 {
		return nil
	}

	logger.Debug("waiting for boot listener readiness", zap.Int64("pending", pending))
	if err := readiness.Wait(ctx); err != nil {
		return NewWaitForListenerReadinessError(err)
	}

	logger.Debug("boot listeners ready")
	return nil
}

// ConvertToWappEntries converts registry entries to wapp entries for packing.
func ConvertToWappEntries(entries []regapi.Entry) []wapp.Entry {
	result := make([]wapp.Entry, len(entries))
	for i, e := range entries {
		var data any
		if e.Data != nil {
			data = e.Data.Data()
		}
		result[i] = wapp.Entry{
			ID:   wapp.NewID(e.ID.NS, e.ID.Name),
			Kind: e.Kind,
			Meta: wapp.Metadata(e.Meta),
			Data: data,
		}
	}
	return result
}

// registerWappWithEmbedRegistry registers .wapp files with the embed registry for fs.embed support.
// Files are kept open and tracked by the registry for cleanup on close.
func registerWappWithEmbedRegistry(ctx context.Context, paths []string, logger *zap.Logger) error {
	embedReg := embedpkg.GetRegistryFromContext(ctx)
	if embedReg == nil {
		return nil // No embed registry, skip
	}

	for _, path := range paths {
		if filepath.Ext(path) != ".wapp" {
			continue
		}

		f, err := os.Open(path)
		if err != nil {
			return NewOpenWappError(path, err)
		}

		reader, err := wapp.NewReader(f)
		if err != nil {
			f.Close()
			return NewReadWappError(path, err)
		}

		if err := embedReg.Register(path, reader, f); err != nil {
			f.Close()
			return NewRegisterEmbedResourcesError(err)
		}

		logger.Debug("registered wapp with embed registry", zap.String("path", path))
	}

	return nil
}
