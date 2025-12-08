// Package entries handles loading registry entries from lock files and managing module installation.
package entries

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/boot/deps/storage"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"go.uber.org/zap"
)

// LoadFromLockFile loads application entries from the lock file and applies them to the registry.
// This function executes between the Load and Start phases of the boot process.
// If modules are missing, it auto-installs them before loading entries.
func LoadFromLockFile(ctx context.Context, logger *zap.Logger, verbose bool) error {
	lockFilePath := "wippy.lock"

	lockPath, err := lock.Find(".", lockFilePath)
	if err != nil {
		logger.Info("no lock file found, starting with empty registry")
		return nil
	}

	logger.Info("loading entries from lock file", zap.String("path", lockPath))

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(err)
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(err)
	}

	if err := EnsureModulesInstalled(ctx, lockPath, logger); err != nil {
		return NewEnsureModulesInstalledError(err)
	}

	paths := lockObj.GetLoadPaths()
	logger.Debug("load paths from lock file", zap.Strings("paths", paths))

	entries, err := loadEntriesFromPaths(ctx, paths, logger)
	if err != nil {
		return NewLoadEntriesFromPathsError(err)
	}

	logger.Info("loaded entries", zap.Int("count", len(entries)))

	if err := loadEntriesToRegistry(ctx, entries, logger, verbose); err != nil {
		return NewLoadEntriesToRegistryError(err)
	}

	logger.Info("entries loaded to registry successfully")
	return nil
}

// EnsureModulesInstalled checks if modules from the lock file are installed,
// and auto-installs them if missing.
func EnsureModulesInstalled(ctx context.Context, lockPath string, logger *zap.Logger) error {
	lockObj, err := lock.New(lockPath)
	if err != nil {
		return NewLoadLockFileError(err)
	}

	if err := lock.Validate(lockObj); err != nil {
		return NewInvalidLockFileError(err)
	}

	modules := lockObj.GetModules()
	if len(modules) == 0 {
		return nil
	}

	lockDir := filepath.Dir(lockPath)
	vendorPath := filepath.Join(lockDir, lockObj.GetVendorPath())
	logger.Debug("checking modules installation", zap.String("vendor_path", vendorPath))

	allInstalled := true
	for _, mod := range modules {
		name, err := graph.ParseName(mod.Name)
		if err != nil {
			logger.Warn("failed to parse module name", zap.String("module", mod.Name), zap.Error(err))
			continue
		}

		modulePath := lock.ModulePath(name, mod.Version)
		fullPath := filepath.Join(vendorPath, modulePath)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			allInstalled = false
			logger.Info("module not found, will auto-install", zap.String("module", mod.Name))
			break
		}
	}

	if allInstalled {
		logger.Debug("all modules already installed")
		return nil
	}

	logger.Info("auto-installing missing modules")

	registryClient := appinit.GetRegistryClient(ctx)
	if registryClient == nil {
		return ErrRegistryClientNotFound
	}

	storageImpl := storage.NewFileSystemStorage(vendorPath)

	for _, mod := range modules {
		name, err := graph.ParseName(mod.Name)
		if err != nil {
			logger.Warn("failed to parse module name", zap.String("module", mod.Name), zap.Error(err))
			continue
		}

		modulePath := lock.ModulePath(name, mod.Version)

		exists, err := storageImpl.Exists(modulePath)
		if err != nil {
			logger.Warn("failed to check module", zap.String("module", mod.Name), zap.Error(err))
			continue
		}

		if exists {
			continue
		}

		logger.Info("downloading module",
			zap.String("module", mod.Name),
			zap.String("version", mod.Version))

		if mod.Hash == "" {
			return ErrModuleMissingHash
		}

		results, err := registryClient.Download(ctx, []string{mod.Hash})
		if err != nil {
			return NewDownloadModuleError(mod.Name, err)
		}

		if len(results) == 0 {
			return ErrNoContentDownloaded
		}

		if err := storageImpl.StoreProtoFiles(modulePath, results[0].Files); err != nil {
			return NewStoreModuleError(mod.Name, err)
		}
	}

	logger.Info("modules installed successfully")
	return nil
}

// loadEntriesFromPaths loads registry entries from the specified directories using the LoadDirs pipeline stage.
func loadEntriesFromPaths(ctx context.Context, paths []string, logger *zap.Logger) ([]regapi.Entry, error) {
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
		if _, err := os.Stat(path); os.IsNotExist(err) {
			logger.Warn("path not found, skipping", zap.String("path", path))
			continue
		}

		dirFS := os.DirFS(path)
		loadedEntries, err := ldr.LoadFS(ctx, dirFS)
		if err != nil {
			return nil, NewLoadFromPathError(path, err)
		}

		entries = append(entries, loadedEntries...)
	}

	pipeline := build.New(
		stages.Override(),
		stages.Disable(),
		stages.Link(),
	)

	if err := pipeline.Execute(ctx, &entries); err != nil {
		return nil, NewExecutePipelineError(err)
	}

	return entries, nil
}

// loadEntriesToRegistry loads entries into the registry using LoadState to restore from history.
func loadEntriesToRegistry(ctx context.Context, entries []regapi.Entry, logger *zap.Logger, _ bool) error {
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
		return NewLoadStateError(err)
	}

	logger.Debug("registry state loaded", zap.Uint("version", head.ID()))
	return nil
}
