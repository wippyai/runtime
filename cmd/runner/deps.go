package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/moduleloader"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/registry/loader"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
	"go.uber.org/zap"
)

// ModuleOperation represents a single module operation
type ModuleOperation struct {
	Name    string
	Version string
	Action  string // "installed", "updated", "removed"
}

// OperationStats tracks module operations
type OperationStats struct {
	Installed int
	Updated   int
	Removed   int
	Modules   []ModuleOperation
}

// DependencyManager handles dependency installation and updates
type DependencyManager struct {
	config *Config
	logger *zap.Logger
}

// NewDependencyManager creates a new dependency manager
func NewDependencyManager(config *Config, logger *zap.Logger) *DependencyManager {
	return &DependencyManager{
		config: config,
		logger: logger,
	}
}

// InstallDependencies installs dependencies from lock file
func (dm *DependencyManager) InstallDependencies(ctx context.Context) error {
	// Determine lock file path
	lockPath, err := moduleloader.FindLockFile(dm.config.FolderPath, dm.config.LockFile)
	if err != nil {
		return fmt.Errorf("find lock file: %w", err)
	}

	// Check if lock file exists
	if lockPath == "" {
		return fmt.Errorf("no lock file found in project directory: %s", dm.config.FolderPath)
	}

	// Load lock file
	lockFile, err := moduleloader.LoadLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("load lock file: %w", err)
	}

	// Install dependencies using the new method that handles replacements
	stats, err := dm.installModulesFromLockFile(ctx, lockFile, lockPath)
	if err != nil {
		return fmt.Errorf("install modules: %w", err)
	}

	// Display final summary
	dm.displayOperationStats(stats)
	dm.logger.Info("Installation completed")
	return nil
}

// UpdateDependencies updates dependencies and regenerates lock file
func (dm *DependencyManager) UpdateDependencies(ctx context.Context) error {
	dm.logger.Info("Updating dependencies")

	// First, try to load existing lock file to get src directory
	var srcDir string
	existingLockPath := filepath.Join(dm.config.FolderPath, dm.config.LockFile)
	if existingLock, err := moduleloader.LoadLockFile(existingLockPath); err == nil {
		srcDir = existingLock.Directories.Src
		dm.logger.Debug("Using src directory from lock file", zap.String("src_dir", srcDir))
	} else {
		// Use default src directory if no existing lock file
		srcDir = "."
		dm.logger.Debug("Using default src directory", zap.String("src_dir", srcDir))
	}

	// Load entries from the src directory (not root directory)
	entries, err := dm.loadRegistryEntries(ctx, srcDir)
	if err != nil {
		return fmt.Errorf("load registry entries: %w", err)
	}

	// Create module loader manager with loaded entries
	dm.logger.Debug("Creating module loader manager with entries", zap.Int("entries_count", len(entries)))
	registryLoader := dm.createModuleLoaderManagerWithEntries(entries)

	// Load dependencies with latest versions
	dm.logger.Debug("Loading dependencies with registry loader")
	loadResult, err := registryLoader.Load(ctx)
	if err != nil {
		return fmt.Errorf("load dependencies: %w", err)
	}

	dm.logger.Debug("Registry loader completed",
		zap.Int("modules_count", len(loadResult.Modules)))

	// Install updated dependencies
	if err := dm.installModules(ctx, loadResult); err != nil {
		return fmt.Errorf("install modules: %w", err)
	}

	// Save new lock file
	dm.logger.Debug("Converting loadResult to lock file",
		zap.Int("modules_count", len(loadResult.Modules)))

	// Log each module for debugging
	for i, module := range loadResult.Modules {
		dm.logger.Debug("Module in loadResult",
			zap.Int("index", i),
			zap.String("name", module.Name.String()),
			zap.String("version", module.Version),
			zap.String("path", module.Path))
	}

	// Get directory structure and preserve replacements from existing lock file
	var modulesDir string
	var existingReplacements []moduleloader.Replacement
	if existingLock, err := moduleloader.LoadLockFile(existingLockPath); err == nil {
		modulesDir = existingLock.Directories.Modules
		existingReplacements = existingLock.Replacements
		dm.logger.Debug("Preserving existing replacements",
			zap.Int("count", len(existingReplacements)))
	} else {
		// Use default modules directory if no existing lock file
		modulesDir = ".wippy"
	}

	lockFile := moduleloader.ConvertToLockFile(loadResult, modulesDir, srcDir)

	// Preserve existing replacements
	if len(existingReplacements) > 0 {
		lockFile.Replacements = existingReplacements
		dm.logger.Debug("Restored existing replacements",
			zap.Int("count", len(lockFile.Replacements)))
	}

	dm.logger.Debug("Lock file created",
		zap.String("modules_dir", lockFile.Directories.Modules),
		zap.String("src_dir", lockFile.Directories.Src),
		zap.Int("modules_count", len(lockFile.Modules)))

	// Log each module in lock file for debugging
	for i, module := range lockFile.Modules {
		dm.logger.Debug("Module in lock file",
			zap.Int("index", i),
			zap.String("name", module.Name),
			zap.String("version", module.Version))
	}

	lockPath := filepath.Join(dm.config.FolderPath, dm.config.LockFile)

	if err := lockFile.SaveLockFile(lockPath); err != nil {
		return fmt.Errorf("save lock file: %w", err)
	}

	return nil
}

// loadRegistryEntries loads registry entries from the specified directory
func (dm *DependencyManager) loadRegistryEntries(ctx context.Context, srcDir string) ([]regapi.Entry, error) {
	// For testing purposes, create mock entries if no real entries are loaded
	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	folderLoader := loader.NewLoader(dtt, dm.logger, interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadFile),
	))

	// Resolve the full path to the src directory
	srcPath := filepath.Join(dm.config.FolderPath, srcDir)

	// Create filesystem from the src directory
	fsys := os.DirFS(srcPath)

	// Load entries from the filesystem
	entries, err := folderLoader.LoadFS(ctx, fsys)
	if err != nil {
		return nil, fmt.Errorf("load entries from directory %s: %w", srcPath, err)
	}

	dm.logger.Debug("Loaded entries from directory",
		zap.String("directory", srcPath),
		zap.String("src_dir", srcDir),
		zap.Int("count", len(entries)))

	// Log some sample entries for debugging
	for i, entry := range entries {
		if i < 5 { // Log first 5 entries
			dm.logger.Debug("Sample entry",
				zap.Int("index", i),
				zap.String("id", entry.ID.String()),
				zap.String("kind", entry.Kind))
		}
	}

	return entries, nil
}

// createModuleLoaderManagerWithEntries creates a module loader manager with provided entries
func (dm *DependencyManager) createModuleLoaderManagerWithEntries(entries []regapi.Entry) *moduleloader.Manager {
	baseURL := "https://modules.wippy.ai"
	if modulesURL := os.Getenv("WIPPY_MODULES_URL"); modulesURL != "" {
		baseURL = modulesURL
	}

	dm.logger.Debug("Creating registry loader with entries",
		zap.String("base_url", baseURL),
		zap.Int("entries_count", len(entries)))

	client := &http.Client{}
	organizationClient := identityv1connect.NewOrganizationServiceClient(client, baseURL)
	moduleClient := modulev1connect.NewModuleServiceClient(client, baseURL)
	labelClient := modulev1connect.NewLabelServiceClient(client, baseURL)
	commitClient := modulev1connect.NewCommitServiceClient(client, baseURL)
	downloadClient := modulev1connect.NewDownloadServiceClient(client, baseURL)

	registryLoader := moduleloader.NewEntryLoader(entries, dm.logger)

	dm.logger.Debug("Created entry loader, creating manager")

	return moduleloader.NewManager(
		organizationClient,
		moduleClient,
		commitClient,
		labelClient,
		downloadClient,
		registryLoader,
		moduleloader.VendorFolder,
	)
}

// createModuleLoaderManagerWithLoader creates a module loader manager with provided loader
func (dm *DependencyManager) createModuleLoaderManagerWithLoader(loader moduleloader.ManifestLoader) *moduleloader.Manager {
	baseURL := "https://modules.wippy.ai"
	if modulesURL := os.Getenv("WIPPY_MODULES_URL"); modulesURL != "" {
		baseURL = modulesURL
	}

	client := &http.Client{}
	organizationClient := identityv1connect.NewOrganizationServiceClient(client, baseURL)
	moduleClient := modulev1connect.NewModuleServiceClient(client, baseURL)
	labelClient := modulev1connect.NewLabelServiceClient(client, baseURL)
	commitClient := modulev1connect.NewCommitServiceClient(client, baseURL)
	downloadClient := modulev1connect.NewDownloadServiceClient(client, baseURL)

	return moduleloader.NewManager(
		organizationClient,
		moduleClient,
		commitClient,
		labelClient,
		downloadClient,
		loader,
		moduleloader.VendorFolder,
	)
}

// installModules installs the modules from load result
func (dm *DependencyManager) installModules(ctx context.Context, loadResult *moduleloader.LoadResult) error {
	if len(loadResult.Modules) == 0 {
		dm.logger.Info("No modules to install")
		return nil
	}

	// Install each module
	for _, module := range loadResult.Modules {
		dm.logger.Debug("Processing module for installation",
			zap.String("name", module.Name.String()),
			zap.String("version", module.Version),
			zap.String("path", module.Path))

		if err := dm.installSingleModule(ctx, module); err != nil {
			return fmt.Errorf("install module %s: %w", module.Name.String(), err)
		}
	}
	return nil
}

// installModulesFromLockFile installs modules from a lock file, handling replacements
func (dm *DependencyManager) installModulesFromLockFile(ctx context.Context, lockFile *moduleloader.LockFile, lockPath string) (*OperationStats, error) {
	if len(lockFile.Modules) == 0 {
		dm.logger.Info("No modules to install")
		return &OperationStats{}, nil
	}

	// Create a map of replacements for quick lookup
	replacements := make(map[string]string)
	for _, replacement := range lockFile.Replacements {
		replacements[replacement.From] = replacement.To
	}

	// Track operations
	stats := &OperationStats{}

	// Install each module
	for _, module := range lockFile.Modules {
		dm.logger.Debug("Processing module for installation",
			zap.String("name", module.Name),
			zap.String("version", module.Version))

		// Check if module is already installed to determine if it's an update
		wasInstalled := dm.isModuleInstalledFromLockFile(module, lockFile)

		if err := dm.installModuleFromLockFile(ctx, module, replacements, lockPath); err != nil {
			return nil, fmt.Errorf("install module %s: %w", module.Name, err)
		}

		// Record the operation
		var action string
		if wasInstalled {
			stats.Updated++
			action = "updated"
		} else {
			stats.Installed++
			action = "installed"
		}

		stats.Modules = append(stats.Modules, ModuleOperation{
			Name:    module.Name,
			Version: module.Version,
			Action:  action,
		})
	}

	return stats, nil
}

// isModuleInstalledFromLockFile checks if a module is already installed based on lock file
func (dm *DependencyManager) isModuleInstalledFromLockFile(module moduleloader.LockedModule, lockFile *moduleloader.LockFile) bool {
	// Check if any module directory exists for this module
	moduleBaseDir := filepath.Join(lockFile.Directories.Modules, module.Name)
	if _, err := os.Stat(moduleBaseDir); err != nil {
		return false
	}

	// Look for module directories with commit hash pattern
	entries, err := os.ReadDir(moduleBaseDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), module.Name+"@") {
			return true
		}
	}

	return false
}

// installSingleModule installs a single module using moduleloader.Manager
func (dm *DependencyManager) installSingleModule(ctx context.Context, module moduleloader.LoadedModule) error {
	dm.logger.Debug("Installing module",
		zap.String("name", module.Name.String()),
		zap.String("version", module.Version),
		zap.String("path", module.Path))

	// Check if module is already installed
	if dm.isModuleInstalled(module) {
		dm.logger.Debug("Module already installed, skipping",
			zap.String("name", module.Name.String()),
			zap.String("version", module.Version))
		return nil
	}

	// Create a manifest with just this module for installation
	manifest := &moduleloader.Manifest{
		Dependencies: []moduleloader.ManifestDependency{
			{
				Name:    module.Name,
				Version: module.Version,
			},
		},
	}

	// Create a simple loader that returns our manifest
	loader := &singleModuleLoader{manifest: manifest}

	// Create module loader manager (uses default .wippy directory)
	registryLoader := dm.createModuleLoaderManagerWithLoader(loader)

	// Load (download and install) the module
	loadResult, err := registryLoader.Load(ctx)
	if err != nil {
		return fmt.Errorf("install module %s: %w", module.Name.String(), err)
	}

	if len(loadResult.Modules) == 0 {
		return fmt.Errorf("no modules loaded for %s", module.Name.String())
	}

	installedModule := loadResult.Modules[0]
	dm.logger.Debug("Module installed successfully",
		zap.String("name", installedModule.Name.String()),
		zap.String("version", installedModule.Version),
		zap.String("path", installedModule.Path))

	return nil
}

// installModuleFromLockFile installs a single module from lock file, handling replacements
func (dm *DependencyManager) installModuleFromLockFile(ctx context.Context, module moduleloader.LockedModule, replacements map[string]string, lockPath string) error {
	dm.logger.Debug("Installing module from lock file",
		zap.String("name", module.Name),
		zap.String("version", module.Version))

	// Check if this module has a replacement
	if customPath, hasReplacement := replacements[module.Name]; hasReplacement {
		// Use the custom path from replacement
		dm.logger.Info("Using replacement path for module",
			zap.String("module", module.Name),
			zap.String("path", customPath))

		// Resolve the custom path relative to the lock file location
		resolvedPath := filepath.Join(filepath.Dir(lockPath), customPath)

		// Check if the replacement path exists
		if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
			return fmt.Errorf("replacement path does not exist: %s (resolved to: %s)", customPath, resolvedPath)
		}

		// For now, just log that we would use the replacement path
		// In a full implementation, we would copy/link the module to the appropriate location
		dm.logger.Info("Module replacement found, would use custom path",
			zap.String("module", module.Name),
			zap.String("custom_path", resolvedPath))

		return nil
	}

	// No replacement, use default installation logic
	// Parse the module name to get organization and module parts
	name, err := moduleloader.ParseName(module.Name)
	if err != nil {
		return fmt.Errorf("invalid module name %s: %w", module.Name, err)
	}

	// Load lock file to get modules directory
	lockFile, err := moduleloader.LoadLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to load lock file to get modules directory: %w", err)
	}

	// Check if module is already installed in the modules directory from lock file
	modulesDir := filepath.Join(filepath.Dir(lockPath), lockFile.Directories.Modules)
	moduleBaseDir := filepath.Join(modulesDir, name.Organization)
	if _, err := os.Stat(moduleBaseDir); err == nil {
		// Look for module directories with commit hash pattern
		entries, err := os.ReadDir(moduleBaseDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() && strings.HasPrefix(entry.Name(), name.Module+"@") {
					dm.logger.Debug("Module already installed, skipping",
						zap.String("name", module.Name),
						zap.String("version", module.Version))
					return nil
				}
			}
		}
	}

	// Create a manifest with just this module for installation
	manifest := &moduleloader.Manifest{
		Dependencies: []moduleloader.ManifestDependency{
			{
				Name:    name,
				Version: module.Version,
			},
		},
	}

	// Create a simple loader that returns our manifest
	loader := &singleModuleLoader{manifest: manifest}

	// Create a custom manager with the modules directory from the lock file
	baseURL := "https://modules.wippy.ai"
	if modulesURL := os.Getenv("WIPPY_MODULES_URL"); modulesURL != "" {
		baseURL = modulesURL
	}

	client := &http.Client{}
	organizationClient := identityv1connect.NewOrganizationServiceClient(client, baseURL)
	moduleClient := modulev1connect.NewModuleServiceClient(client, baseURL)
	labelClient := modulev1connect.NewLabelServiceClient(client, baseURL)
	commitClient := modulev1connect.NewCommitServiceClient(client, baseURL)
	downloadClient := modulev1connect.NewDownloadServiceClient(client, baseURL)

	// Create the manager with the custom modules directory
	registryLoader := moduleloader.NewManager(
		organizationClient,
		moduleClient,
		commitClient,
		labelClient,
		downloadClient,
		loader,
		lockFile.Directories.Modules, // Use the modules directory from lock file
	)

	// Load (download and install) the module
	loadResult, err := registryLoader.Load(ctx)
	if err != nil {
		return fmt.Errorf("install module from lock file %s: %w", module.Name, err)
	}

	if len(loadResult.Modules) == 0 {
		return fmt.Errorf("no modules loaded for %s", module.Name)
	}

	installedModule := loadResult.Modules[0]
	dm.logger.Debug("Module installed successfully",
		zap.String("name", installedModule.Name.String()),
		zap.String("version", installedModule.Version),
		zap.String("path", installedModule.Path))

	return nil
}

// singleModuleLoader is a simple loader that returns a predefined manifest
type singleModuleLoader struct {
	manifest *moduleloader.Manifest
}

func (l *singleModuleLoader) LoadManifest(_ context.Context) (*moduleloader.Manifest, error) {
	return l.manifest, nil
}

// isModuleInstalled checks if a module is already installed
func (dm *DependencyManager) isModuleInstalled(module moduleloader.LoadedModule) bool {
	// Check if any module directory exists for this module
	moduleBaseDir := filepath.Join(moduleloader.VendorFolder, module.Name.Organization)
	if _, err := os.Stat(moduleBaseDir); err != nil {
		return false
	}

	// Look for module directories with commit hash pattern
	entries, err := os.ReadDir(moduleBaseDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), module.Name.Module+"@") {
			return true
		}
	}

	return false
}

// displayModules displays modules with operation statistics
func (dm *DependencyManager) displayModules(installed, updated, removed int, modules []ModuleOperation) {
	if len(modules) == 0 {
		dm.logger.Info("No modules processed")
		return
	}

	dm.logger.Info(fmt.Sprintf("%d installs, %d updates, %d removals:",
		installed, updated, removed))

	for _, module := range modules {
		dm.logger.Info(fmt.Sprintf("- %s: %s", module.Name, module.Version))
	}
}

// displayOperationStats displays operation statistics with module details
func (dm *DependencyManager) displayOperationStats(stats *OperationStats) {
	if stats == nil {
		dm.logger.Info("No modules processed")
		return
	}

	dm.displayModules(stats.Installed, stats.Updated, stats.Removed, stats.Modules)
}
