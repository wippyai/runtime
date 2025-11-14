package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ponyruntime/pony/api/payload"
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/boot/build"
	"github.com/ponyruntime/pony/boot/build/stages"
	cliboot "github.com/ponyruntime/pony/boot/cli"
	"github.com/ponyruntime/pony/boot/loader"
	"github.com/ponyruntime/pony/deps/graph"
	"github.com/ponyruntime/pony/deps/lock"
	"github.com/ponyruntime/pony/deps/storage"
	regtop "github.com/ponyruntime/pony/system/registry/topology"
	"go.uber.org/zap"
)

// loadEntriesFromLockFile loads application entries from the lock file and applies them to the registry.
// This function executes between the Load and Start phases of the boot process.
// If modules are missing, it auto-installs them before loading entries.
func loadEntriesFromLockFile(ctx context.Context, logger *zap.Logger) error {
	lockFilePath := "wippy.lock"

	lockPath, err := lock.Find(".", lockFilePath)
	if err != nil {
		logger.Info("no lock file found, starting with empty registry")
		return nil
	}

	logger.Info("loading entries from lock file", zap.String("path", lockPath))

	lockObj, err := lock.New(lockPath)
	if err != nil {
		return fmt.Errorf("load lock file: %w", err)
	}

	if err := ensureModulesInstalled(ctx, lockPath, lockFilePath, logger); err != nil {
		return fmt.Errorf("ensure modules installed: %w", err)
	}

	paths := lockObj.GetLoadPaths()
	logger.Debug("load paths from lock file", zap.Strings("paths", paths))

	entries, err := loadEntriesFromPaths(ctx, paths, logger)
	if err != nil {
		return fmt.Errorf("load entries from paths: %w", err)
	}

	logger.Info("loaded entries", zap.Int("count", len(entries)))

	if err := applyEntriesToRegistry(ctx, entries, logger); err != nil {
		return fmt.Errorf("apply entries to registry: %w", err)
	}

	logger.Info("entries applied to registry successfully")
	return nil
}

// ensureModulesInstalled checks if modules from the lock file are installed,
// and auto-installs them if missing.
func ensureModulesInstalled(ctx context.Context, lockPath, lockFilePath string, logger *zap.Logger) error {
	lockObj, err := lock.New(lockPath)
	if err != nil {
		return fmt.Errorf("load lock file: %w", err)
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

	registryClient := cliboot.GetRegistryClient(ctx)
	if registryClient == nil {
		return fmt.Errorf("registry client not found in context")
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
			return fmt.Errorf("module %s has no hash in lock file", mod.Name)
		}

		results, err := registryClient.Download(ctx, []string{mod.Hash})
		if err != nil {
			return fmt.Errorf("download module %s: %w", mod.Name, err)
		}

		if len(results) == 0 {
			return fmt.Errorf("no content downloaded for module %s", mod.Name)
		}

		if err := storageImpl.StoreProtoFiles(modulePath, results[0].Files); err != nil {
			return fmt.Errorf("store module %s: %w", mod.Name, err)
		}
	}

	logger.Info("modules installed successfully")
	return nil
}

// loadEntriesFromPaths loads registry entries from the specified directories using the LoadDirs pipeline stage.
func loadEntriesFromPaths(ctx context.Context, paths []string, logger *zap.Logger) ([]regapi.Entry, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, fmt.Errorf("transcoder not found in context")
	}

	ldrInterface := cliboot.GetLoader(ctx)
	if ldrInterface == nil {
		return nil, fmt.Errorf("loader not found in context")
	}
	ldr, ok := ldrInterface.(*loader.Loader)
	if !ok {
		return nil, fmt.Errorf("loader has unexpected type")
	}

	entries := []regapi.Entry{}

	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			logger.Warn("path not found, skipping", zap.String("path", path))
			continue
		}

		dirFS := os.DirFS(path)
		loadedEntries, err := ldr.LoadFS(ctx, dirFS)
		if err != nil {
			return nil, fmt.Errorf("load from %s: %w", path, err)
		}

		entries = append(entries, loadedEntries...)
	}

	pipeline := build.New(
		stages.Override(),
		stages.Disable(),
		stages.Link(),
	)

	if err := pipeline.Execute(ctx, &entries); err != nil {
		return nil, fmt.Errorf("execute pipeline: %w", err)
	}

	return entries, nil
}

// applyEntriesToRegistry converts entries to a ChangeSet and applies them to the registry.
func applyEntriesToRegistry(ctx context.Context, entries []regapi.Entry, logger *zap.Logger) error {
	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		return fmt.Errorf("registry not found in context")
	}

	resolver := regapi.GetResolver(ctx)
	if resolver == nil {
		return fmt.Errorf("dependency resolver not found in context")
	}

	// Use CreateChangeSetFromEntries which properly sorts by dependencies
	changeSet, err := regtop.CreateChangeSetFromEntries(entries, resolver)
	if err != nil {
		return fmt.Errorf("build change set: %w", err)
	}

	version, err := reg.Apply(ctx, changeSet)
	if err != nil {
		return fmt.Errorf("apply change set: %w", err)
	}

	logger.Debug("registry updated", zap.Any("version", version))
	return nil
}
