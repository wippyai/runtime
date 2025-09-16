package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
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

// ModuleOperation represents a single module operation with version tracking
type ModuleOperation struct {
	Name       string // Module name (e.g., "wippy/actor")
	Version    string // Current version
	OldVersion string // Previous version for updates
	Action     string // Operation type: "installed", "updated", "removed"
}

// DependencyManager handles dependency installation, updates, and cleanup
type DependencyManager struct {
	config         *Config
	logger         *zap.Logger
	registryLoader RegistryLoader
	lockFileMgr    LockFileManager
}

// NewDependencyManager creates a new dependency manager with default implementations
func NewDependencyManager(config *Config, logger *zap.Logger) *DependencyManager {
	return &DependencyManager{
		config:         config,
		logger:         logger,
		registryLoader: &defaultRegistryLoader{config: config, logger: logger},
		lockFileMgr:    &defaultLockFileManager{logger: logger},
	}
}

// InstallDependencies installs all dependencies from the lock file
// Creates a statistics tracker and displays operation results
func (dm *DependencyManager) InstallDependencies(ctx context.Context) error {
	dm.logger.Info(LogInstallingDependencies)

	// Create statistics tracker with verbose flag from config
	stats := NewModuleOperationStats(dm.config.Verbose)
	return dm.InstallDependenciesWithStats(ctx, stats)
}

// InstallDependenciesWithStats installs all dependencies from the lock file with provided statistics tracker
func (dm *DependencyManager) InstallDependenciesWithStats(ctx context.Context, stats *ModuleOperationStats) error {
	// Load lock file
	lockFile, _, err := dm.loadLockFile()
	if err != nil {
		return err
	}

	// First cleanup unused modules to remove old versions
	err = dm.CleanupAllUnusedModules(ctx, lockFile.Directories.Src, lockFile, stats)
	if err != nil {
		return err
	}

	// Then install modules and collect statistics
	err = dm.installModulesFromLockFileSilent(ctx, lockFile, lockFile.Directories.Src, stats)
	if err != nil {
		return err
	}

	// Display combined statistics
	dm.ShowResults(stats)

	dm.logger.Info(LogInstallationCompleted)
	return nil
}

// UpdateDependencies updates dependencies and regenerates lock file
// Delegates to UpdateDependenciesWithRemovedModules for full functionality
func (dm *DependencyManager) UpdateDependencies(ctx context.Context) error {
	stats := NewModuleOperationStats(dm.config.Verbose)
	return dm.UpdateDependenciesWithStats(ctx, stats)
}

// UpdateDependenciesWithStats updates dependencies and regenerates lock file with provided statistics tracker
func (dm *DependencyManager) UpdateDependenciesWithStats(ctx context.Context, stats *ModuleOperationStats) error {
	return dm.UpdateDependenciesWithRemovedModules(ctx, stats)
}

// UpdateDependenciesWithRemovedModules performs a complete dependency update:
// 1. Loads existing lock file and processes dependencies
// 2. Generates new lock file with latest versions
// 3. Cleans up unused modules
// 4. Displays comprehensive operation statistics
func (dm *DependencyManager) UpdateDependenciesWithRemovedModules(ctx context.Context, stats *ModuleOperationStats) error {
	dm.logger.Info(LogUpdatingDependencies)

	// Load existing lock file to compare changes
	existingLock, srcDir, modulesDir, existingReplacements := dm.loadExistingLockFile()

	// Load and process dependencies
	loadResult, err := dm.loadAndProcessDependencies(ctx, srcDir)
	if err != nil {
		return err
	}

	// Add module statistics from load result
	stats.AddModuleStats(loadResult.ModuleStats)

	// Convert to new lock file
	newLockFile := moduleloader.ConvertToLockFile(loadResult, modulesDir, srcDir)

	// Preserve existing replacements
	if len(existingReplacements) > 0 {
		newLockFile.Replacements = existingReplacements
		dm.logger.Debug("Restored existing replacements",
			zap.Int("count", len(newLockFile.Replacements)))
	}

	// Calculate and display changes
	if existingLock != nil {
		changes := dm.calculateLockFileChanges(existingLock, newLockFile)
		dm.displayUpdateChanges(changes)
	} else {
		dm.logger.Info("Lock file operations: 0 installs, 0 updates, 0 removals:")
	}

	// Save new lock file
	lockPath := filepath.Join(dm.config.FolderPath, dm.config.LockFile)
	if err := dm.lockFileMgr.SaveLockFile(newLockFile, lockPath); err != nil {
		return fmt.Errorf("save lock file: %w", err)
	}

	// Load lock file for cleanup
	lockFile, err := moduleloader.LoadLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("load lock file: %w", err)
	}

	// Cleanup unused modules and collect statistics
	err = dm.CleanupAllUnusedModules(ctx, lockFile.Directories.Src, lockFile, stats)
	if err != nil {
		return err
	}

	// Add removed modules from lock file changes
	if existingLock != nil {
		oldModules := make(map[string]bool)
		newModules := make(map[string]bool)

		for _, module := range existingLock.Modules {
			oldModules[module.Name] = true
		}
		for _, module := range newLockFile.Modules {
			newModules[module.Name] = true
		}

		for moduleName := range oldModules {
			if !newModules[moduleName] {
				// Find the version from the existing lock file
				version := "unknown"
				for _, module := range existingLock.Modules {
					if module.Name == moduleName {
						version = module.Version
						break
					}
				}
				stats.AddRemoved(moduleName, version)
			}
		}
	}

	// Display combined statistics
	dm.logger.Info(LogInstallingDependencies)
	dm.ShowResults(stats)

	dm.logger.Info(LogUpdatingCompleted)
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
		dm.logger,
		moduleloader.VendorFolder,
	)
}

// installModulesFromLockFileSilent installs modules from lock file without verbose output
// Records operations in the provided statistics tracker for later display
func (dm *DependencyManager) installModulesFromLockFileSilent(ctx context.Context, lockFile *moduleloader.LockFile, lockPath string, stats *ModuleOperationStats) error {
	if len(lockFile.Modules) == 0 {
		return nil
	}

	// Create a map of replacements for quick lookup
	replacements := make(map[string]string)
	for _, replacement := range lockFile.Replacements {
		replacements[replacement.From] = replacement.To
	}

	// Track operations - stats parameter is already provided

	// Install each module silently
	for _, module := range lockFile.Modules {
		dm.logger.Debug("Processing module for installation",
			zap.String("name", module.Name),
			zap.String("version", module.Version))

		// Check if module is already installed to determine if it's an update
		wasInstalled, oldVersion := dm.isModuleInstalledFromLockFile(module, lockFile)

		// Install module with status output
		actuallyInstalled, err := dm.installModuleFromLockFileWithStatus(ctx, module, replacements, lockPath, lockFile, stats)
		if err != nil {
			return fmt.Errorf("install module %s: %w", module.Name, err)
		}

		// Record the operation based on what actually happened
		if actuallyInstalled {
			// Record the operation using the appropriate method
			if wasInstalled {
				// Check if it's actually an update (different version) or just a reinstall (same version)
				if oldVersion != module.Version {
					stats.AddUpdated(module.Name, module.Version, oldVersion)
				} else {
					stats.AddSkipped(module.Name, module.Version)
				}
			} else {
				stats.AddInstalled(module.Name, module.Version)
			}
		} else {
			// Module was skipped (already installed or other reason)
			// Check if it was already installed to determine the reason
			if wasInstalled {
				// Module was already installed with the same version - skip
				stats.AddSkipped(module.Name, module.Version)
			}
			// If wasInstalled is false and actuallyInstalled is false,
			// it means there was an error or the module was skipped for another reason
			// The AddSkipped call is already handled in installModuleFromLockFileWithStatus
		}
	}

	return nil
}

// isModuleInstalledFromLockFile checks if a module is already installed based on lock file
// Returns true if installed and the old version string
func (dm *DependencyManager) isModuleInstalledFromLockFile(module moduleloader.LockedModule, lockFile *moduleloader.LockFile) (bool, string) {
	// Parse module name to get organization and module parts
	name, err := moduleloader.ParseName(module.Name)
	if err != nil {
		dm.logger.Debug("Failed to parse module name", zap.Error(err))
		return false, ""
	}

	// Check if any module directory exists for this module
	// Structure: .wippy/organization/ (contains module@hash directories)
	organizationDir := filepath.Join(lockFile.Directories.Modules, name.Organization)

	if _, err := os.Stat(organizationDir); err != nil {
		return false, ""
	}

	// Look for module directories with commit hash pattern
	entries, err := os.ReadDir(organizationDir)
	if err != nil {
		return false, ""
	}

	// Check if the exact module with the same hash is already installed
	expectedDirName := name.Module + "@" + module.Hash

	for _, entry := range entries {
		if entry.IsDir() && entry.Name() == expectedDirName {
			// Module is already installed with the same hash (same version)
			return true, module.Version
		}
	}

	// Check if module is installed with different hash (different version)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), name.Module+"@") {
			// Extract hash from directory name (format: module@hash)
			dirName := entry.Name()
			if atIndex := strings.LastIndex(dirName, "@"); atIndex != -1 {
				oldHash := dirName[atIndex+1:]
				// Find the version for this hash in the lock file
				for _, lockModule := range lockFile.Modules {
					if lockModule.Name == module.Name && lockModule.Hash == oldHash {
						return true, lockModule.Version
					}
				}
				return true, "unknown"
			}
		}
	}
	return false, ""
}

// installModuleFromLockFileWithStatus installs a single module from lock file with status reporting
// Returns true if module was actually installed, false if it was skipped
func (dm *DependencyManager) installModuleFromLockFileWithStatus(ctx context.Context, module moduleloader.LockedModule, replacements map[string]string, _ string, lockFile *moduleloader.LockFile, _ *ModuleOperationStats) (bool, error) {
	dm.logger.Debug("Installing module from lock file",
		zap.String("name", module.Name),
		zap.String("version", module.Version))

	// Check if this module has a replacement
	if _, hasReplacement := replacements[module.Name]; hasReplacement {
		// Use the custom path from replacement
		return true, nil
	}

	// Parse the module name to get organization and module parts
	name, err := moduleloader.ParseName(module.Name)
	if err != nil {
		return false, fmt.Errorf("invalid module name %s: %w", module.Name, err)
	}

	// Check if module is already installed but with different version (skip)
	if wasInstalled, _ := dm.isModuleInstalledFromLockFile(module, lockFile); wasInstalled {
		return false, nil // Already installed with different version, skip
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
		dm.logger,
		lockFile.Directories.Modules, // Use the modules directory from lock file
	)

	// Load (download and install) the module
	loadResult, err := registryLoader.Load(ctx)
	if err != nil {
		return false, fmt.Errorf("install module from lock file %s: %w", module.Name, err)
	}

	if len(loadResult.Modules) == 0 {
		return false, fmt.Errorf("no modules loaded for %s", module.Name)
	}

	installedModule := loadResult.Modules[0]
	dm.logger.Debug("Module installed successfully",
		zap.String("name", installedModule.Name.String()),
		zap.String("version", installedModule.Version),
		zap.String("path", installedModule.Path))

	return true, nil
}

// singleModuleLoader is a simple loader that returns a predefined manifest
type singleModuleLoader struct {
	manifest *moduleloader.Manifest
}

func (l *singleModuleLoader) LoadManifest(_ context.Context) (*moduleloader.Manifest, error) {
	return l.manifest, nil
}

// LockFileChanges represents changes between old and new lock files
type LockFileChanges struct {
	Installed []ModuleOperation
	Updated   []ModuleOperation
	Removed   []ModuleOperation
}

// calculateLockFileChanges calculates the differences between old and new lock files
func (dm *DependencyManager) calculateLockFileChanges(oldLock, newLock *moduleloader.LockFile) *LockFileChanges {
	changes := &LockFileChanges{
		Installed: []ModuleOperation{},
		Updated:   []ModuleOperation{},
		Removed:   []ModuleOperation{},
	}

	// Create maps for quick lookup
	oldModules := make(map[string]string) // name -> version
	newModules := make(map[string]string) // name -> version

	// Populate old modules map
	for _, module := range oldLock.Modules {
		oldModules[module.Name] = module.Version
	}

	// Populate new modules map and find changes
	for _, module := range newLock.Modules {
		newModules[module.Name] = module.Version

		if oldVersion, exists := oldModules[module.Name]; exists {
			// Module exists in both, check if version changed
			if oldVersion != module.Version {
				changes.Updated = append(changes.Updated, ModuleOperation{
					Name:       module.Name,
					Version:    module.Version,
					OldVersion: oldVersion,
					Action:     "updated",
				})
			}
		} else {
			// Module is new
			changes.Installed = append(changes.Installed, ModuleOperation{
				Name:       module.Name,
				Version:    module.Version,
				OldVersion: "",
				Action:     "installed",
			})
		}
	}

	// Find removed modules
	for _, module := range oldLock.Modules {
		if _, exists := newModules[module.Name]; !exists {
			changes.Removed = append(changes.Removed, ModuleOperation{
				Name:       module.Name,
				Version:    "",
				OldVersion: module.Version,
				Action:     "removed",
			})
		}
	}

	return changes
}

// displayUpdateChanges displays the changes in the requested format
func (dm *DependencyManager) displayUpdateChanges(changes *LockFileChanges) {
	if changes == nil {
		dm.logger.Info("Lock file operations: 0 installs, 0 updates, 0 removals:")
		return
	}

	totalInstalls := len(changes.Installed)
	totalUpdates := len(changes.Updated)
	totalRemovals := len(changes.Removed)

	dm.logger.Info(fmt.Sprintf("Lock file operations: %d installs, %d updates, %d removals:",
		totalInstalls, totalUpdates, totalRemovals))

	// Display updated modules first
	for _, module := range changes.Updated {
		// Determine if it's an upgrade or downgrade based on version comparison
		action := "Upgrading"
		if dm.isVersionDowngrade(module.OldVersion, module.Version) {
			action = "Downgrading"
		}
		dm.logger.Info(fmt.Sprintf(" - %s %s: %s => %s", action, module.Name, module.OldVersion, module.Version))
	}

	// Display removed modules
	for _, module := range changes.Removed {
		dm.logger.Info(fmt.Sprintf(" - Removing %s", module.Name))
	}

	// Display installed modules
	for _, module := range changes.Installed {
		dm.logger.Info(fmt.Sprintf(" - Installing %s: %s", module.Name, module.Version))
	}
}

// isVersionDowngrade determines if going from oldVersion to newVersion is a downgrade
func (dm *DependencyManager) isVersionDowngrade(oldVersion, newVersion string) bool {
	// Parse the versions using semver
	oldVer, err := semver.NewVersion(oldVersion)
	if err != nil {
		dm.logger.Warn(fmt.Sprintf("Failed to parse old version '%s' as semver: %v", oldVersion, err))
		return false
	}

	newVer, err := semver.NewVersion(newVersion)
	if err != nil {
		dm.logger.Warn(fmt.Sprintf("Failed to parse new version '%s' as semver: %v", newVersion, err))
		return false
	}

	// Compare versions - if old version is greater than new version, it's a downgrade
	return oldVer.GreaterThan(newVer)
}

// defaultRegistryLoader implements RegistryLoader interface
type defaultRegistryLoader struct {
	config *Config
	logger *zap.Logger
}

func (rl *defaultRegistryLoader) LoadEntries(ctx context.Context, srcDir string) ([]regapi.Entry, error) {
	// Create a temporary DependencyManager to access the methods
	dm := &DependencyManager{config: rl.config, logger: rl.logger}
	return dm.loadRegistryEntries(ctx, srcDir)
}

func (rl *defaultRegistryLoader) CreateManager(entries []regapi.Entry) *moduleloader.Manager {
	// Create a temporary DependencyManager to access the methods
	dm := &DependencyManager{config: rl.config, logger: rl.logger}
	return dm.createModuleLoaderManagerWithEntries(entries)
}

// defaultLockFileManager implements LockFileManager interface
type defaultLockFileManager struct {
	logger *zap.Logger
}

func (lfm *defaultLockFileManager) LoadLockFile(path string) (*moduleloader.LockFile, error) {
	return moduleloader.LoadLockFile(path)
}

func (lfm *defaultLockFileManager) SaveLockFile(lockFile *moduleloader.LockFile, path string) error {
	return lockFile.SaveLockFile(path)
}

func (lfm *defaultLockFileManager) CalculateChanges(_ *moduleloader.LockFile, _ *moduleloader.LockFile) *LockFileChanges {
	return &LockFileChanges{
		Installed: []ModuleOperation{},
		Updated:   []ModuleOperation{},
		Removed:   []ModuleOperation{},
	}
}

// loadLockFile loads the lock file and returns it with the path
func (dm *DependencyManager) loadLockFile() (*moduleloader.LockFile, string, error) {
	// Determine lock file path
	lockPath, err := moduleloader.FindLockFile(dm.config.FolderPath, dm.config.LockFile)
	if err != nil {
		return nil, "", fmt.Errorf("find lock file: %w", err)
	}

	// Check if lock file exists
	if lockPath == "" {
		return nil, "", fmt.Errorf("no lock file found in project directory: %s", dm.config.FolderPath)
	}

	// Load lock file
	lockFile, err := dm.lockFileMgr.LoadLockFile(lockPath)
	if err != nil {
		return nil, "", fmt.Errorf("load lock file: %w", err)
	}

	return lockFile, lockPath, nil
}

// loadAndProcessDependencies loads entries and processes dependencies
func (dm *DependencyManager) loadAndProcessDependencies(ctx context.Context, srcDir string) (*moduleloader.LoadResult, error) {
	// Load entries from the src directory
	entries, err := dm.registryLoader.LoadEntries(ctx, srcDir)
	if err != nil {
		return nil, fmt.Errorf("load registry entries: %w", err)
	}

	// Create module loader manager with loaded entries
	dm.logger.Debug("Creating module loader manager with entries", zap.Int("entries_count", len(entries)))
	registryLoader := dm.registryLoader.CreateManager(entries)

	// Load dependencies with latest versions
	dm.logger.Debug("Loading dependencies with registry loader")
	loadResult, err := registryLoader.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load dependencies: %w", err)
	}

	dm.logger.Debug("Registry loader completed",
		zap.Int("modules_count", len(loadResult.Modules)))

	return loadResult, nil
}

// loadExistingLockFile loads existing lock file and returns its components
func (dm *DependencyManager) loadExistingLockFile() (*moduleloader.LockFile, string, string, []moduleloader.Replacement) {
	existingLockPath := filepath.Join(dm.config.FolderPath, dm.config.LockFile)
	var existingLock *moduleloader.LockFile
	var srcDir string
	var modulesDir string
	var existingReplacements []moduleloader.Replacement

	if existingLockFile, err := moduleloader.LoadLockFile(existingLockPath); err == nil {
		existingLock = existingLockFile
		srcDir = existingLock.Directories.Src
		modulesDir = existingLock.Directories.Modules
		existingReplacements = existingLock.Replacements
		dm.logger.Debug("Using existing lock file",
			zap.String("src_dir", srcDir),
			zap.String("modules_dir", modulesDir),
			zap.Int("modules_count", len(existingLock.Modules)))
	} else {
		// Use default directories if no existing lock file
		srcDir = "."
		modulesDir = ".wippy"
		dm.logger.Debug("No existing lock file, using defaults",
			zap.String("src_dir", srcDir),
			zap.String("modules_dir", modulesDir))
	}

	return existingLock, srcDir, modulesDir, existingReplacements
}

// CleanupUnusedModules removes module directories that are not listed in the new lock file
// Returns a list of removed module names for further use
func (dm *DependencyManager) CleanupUnusedModules(_ context.Context, newLockFile *moduleloader.LockFile) (map[string]string, error) {
	// Get the modules directory from the new lock file
	modulesDir := newLockFile.Directories.Modules
	if modulesDir == "" {
		modulesDir = ".wippy" // fallback to default
	}

	// Resolve the full path to the modules directory
	modulesPath := filepath.Join(dm.config.FolderPath, modulesDir)

	// Check if modules directory exists
	if _, err := os.Stat(modulesPath); os.IsNotExist(err) {
		dm.logger.Debug("Modules directory does not exist, nothing to clean up",
			zap.String("path", modulesPath))
		return make(map[string]string), nil
	}

	// Create a map of modules that should exist according to the new lock file
	expectedModules := make(map[string]bool)
	for _, module := range newLockFile.Modules {
		// Parse module name to get organization and module parts
		name, err := moduleloader.ParseName(module.Name)
		if err != nil {
			dm.logger.Warn("Failed to parse module name, skipping",
				zap.String("name", module.Name),
				zap.Error(err))
			continue
		}

		// Add both possible directory formats to expected modules
		// Format 1: organization/module
		orgModuleDir := filepath.Join(name.Organization, name.Module)
		expectedModules[orgModuleDir] = true

		// Format 2: organization/module@version (if hash is present)
		if module.Hash != "" {
			orgModuleVersionDir := filepath.Join(name.Organization, name.Module+"@"+module.Hash)
			expectedModules[orgModuleVersionDir] = true
		}
	}

	// Also add replacement modules to expected modules
	for _, replacement := range newLockFile.Replacements {
		// For replacements, we need to check if the "to" path exists
		// and add it to expected modules to prevent deletion
		replacementPath := filepath.Join(dm.config.FolderPath, replacement.To)
		if _, err := os.Stat(replacementPath); err == nil {
			// Extract relative path from the full path
			relPath, err := filepath.Rel(modulesPath, replacementPath)
			if err == nil {
				expectedModules[relPath] = true
			}
		}
	}

	// Scan the modules directory for existing module directories
	removedModules := make(map[string]string) // moduleName -> relativePath

	// First, get all organization directories
	orgEntries, err := os.ReadDir(modulesPath)
	if err != nil {
		return nil, fmt.Errorf("read modules directory: %w", err)
	}

	for _, orgEntry := range orgEntries {
		if !orgEntry.IsDir() {
			continue
		}

		orgPath := filepath.Join(modulesPath, orgEntry.Name())

		// Get all module directories within this organization
		moduleEntries, err := os.ReadDir(orgPath)
		if err != nil {
			dm.logger.Warn("Failed to read organization directory, skipping",
				zap.String("org_path", orgPath),
				zap.Error(err))
			continue
		}

		for _, moduleEntry := range moduleEntries {
			if !moduleEntry.IsDir() {
				continue
			}

			modulePath := filepath.Join(orgPath, moduleEntry.Name())
			relPath := filepath.Join(orgEntry.Name(), moduleEntry.Name())

			// Check if this directory should exist according to the new lock file
			if !expectedModules[relPath] {
				// Check if this is a module directory with subdirectories (new format)
				// Look for module subdirectories inside this directory
				moduleSubEntries, err := os.ReadDir(modulePath)
				if err == nil && len(moduleSubEntries) > 0 {
					// This is a module directory with subdirectories
					// Find the actual module subdirectory
					for _, subEntry := range moduleSubEntries {
						if subEntry.IsDir() && strings.HasPrefix(subEntry.Name(), "module-") {
							// This is the actual module subdirectory
							moduleSubRelPath := filepath.Join(relPath, subEntry.Name())

							// Extract module name from the subdirectory path for reporting
							moduleName := dm.extractModuleNameFromPath(moduleSubRelPath)
							if moduleName != "" {
								removedModules[moduleName] = moduleSubRelPath
							}
							break
						}
					}
				} else {
					// This is a simple module directory (old format)
					// Extract module name from the directory path for reporting
					moduleName := dm.extractModuleNameFromPath(relPath)
					if moduleName != "" {
						removedModules[moduleName] = relPath
					}
				}

				dm.logger.Info("removing: ", zap.String("module_path", modulePath))

				// Remove the entire directory (including subdirectories)
				if err := os.RemoveAll(modulePath); err != nil {
					dm.logger.Error("Failed to remove module directory",
						zap.String("path", modulePath),
						zap.Error(err))
					return nil, fmt.Errorf("remove module directory %s: %w", modulePath, err)
				}
			}
		}

		// Check if organization directory is now empty and remove it
		remainingEntries, err := os.ReadDir(orgPath)
		if err == nil && len(remainingEntries) == 0 {
			if err := os.Remove(orgPath); err != nil {
				dm.logger.Debug("Failed to remove empty organization directory",
					zap.String("path", orgPath),
					zap.Error(err))
			}
		}
	}

	return removedModules, nil
}

// extractModuleNameFromPath extracts a module name from a directory path
// Handles formats like "org/module" and "org/module@version"
func (dm *DependencyManager) extractModuleNameFromPath(relPath string) string {
	// Split by path separator
	parts := strings.Split(relPath, string(filepath.Separator))
	if len(parts) < 2 {
		return ""
	}

	// Get organization and module parts
	org := parts[0]
	modulePart := parts[1]

	// Remove version suffix if present (format: module@version)
	module := modulePart
	if atIndex := strings.LastIndex(modulePart, "@"); atIndex != -1 {
		module = modulePart[:atIndex]
	}

	// Return in "org/module" format
	return fmt.Sprintf("%s/%s", org, module)
}

// extractVersionFromPath extracts the version from a module directory path
// Path format: organization/module@hash/module-version
// Returns the semver version from the module folder name or "unknown" if not found
func (dm *DependencyManager) extractVersionFromPath(relPath string) string {
	// Split by path separator
	parts := strings.Split(relPath, string(filepath.Separator))
	if len(parts) < 3 {
		return "unknown"
	}

	// The module folder is the last part of the path
	moduleFolder := parts[len(parts)-1]

	// Extract version from module folder name (format: module-version)
	// Look for the dash before the version pattern
	// Examples: "module-security-0.0.7" -> "0.0.7"
	//           "module-1.2.3" -> "1.2.3"
	//           "module-test-2.0.0-beta.1" -> "2.0.0-beta.1"

	var version string
	// Find the dash before the version (look for pattern: module-name-version)
	// We need to find the dash that separates the module name from the version
	// The version should start with a digit
	for i := len(moduleFolder) - 1; i >= 0; i-- {
		if moduleFolder[i] == '-' {
			// Check if the part after this dash looks like a version (starts with digit)
			potentialVersion := moduleFolder[i+1:]
			if len(potentialVersion) > 0 && potentialVersion[0] >= '0' && potentialVersion[0] <= '9' {
				version = potentialVersion
				break
			}
		}
	}

	if version == "" {
		return "unknown"
	}

	// Basic validation: check if it looks like a semver version
	// Should contain at least one dot and only valid semver characters
	if version == "" || !strings.Contains(version, ".") {
		return "unknown"
	}

	// Check if version contains only valid semver characters (digits, dots, hyphens, and letters for pre-release)
	for _, char := range version {
		if (char < '0' || char > '9') && char != '.' && char != '-' &&
			(char < 'a' || char > 'z') && (char < 'A' || char > 'Z') {
			return "unknown"
		}
	}

	return version
}

// CleanupModuleContent removes unused content from module directories
// This function looks inside module directories and removes subdirectories
// that don't match the expected version from the lock file
func (dm *DependencyManager) CleanupModuleContent(_ context.Context, newLockFile *moduleloader.LockFile) []string {
	// Get the modules directory from the new lock file
	modulesDir := newLockFile.Directories.Modules
	if modulesDir == "" {
		modulesDir = ".wippy" // fallback to default
	}

	// Resolve the full path to the modules directory
	modulesPath := filepath.Join(dm.config.FolderPath, modulesDir)

	// Check if modules directory exists
	if _, err := os.Stat(modulesPath); os.IsNotExist(err) {
		dm.logger.Debug("Modules directory does not exist, nothing to clean up",
			zap.String("path", modulesPath))
		return []string{}
	}

	// Create a map of expected module versions
	expectedVersions := make(map[string]string) // moduleName -> expectedVersion
	for _, module := range newLockFile.Modules {
		expectedVersions[module.Name] = module.Version
	}

	removedContent := []string{}

	// Process each module directory
	for _, module := range newLockFile.Modules {
		// Parse module name to get organization and module parts
		name, err := moduleloader.ParseName(module.Name)
		if err != nil {
			dm.logger.Warn("Failed to parse module name, skipping",
				zap.String("name", module.Name),
				zap.Error(err))
			continue
		}

		// Build module directory path
		var moduleDirName string
		if module.Hash != "" {
			moduleDirName = name.Module + "@" + module.Hash
		} else {
			moduleDirName = name.Module
		}
		moduleDirPath := filepath.Join(modulesPath, name.Organization, moduleDirName)

		// Check if module directory exists
		if _, err := os.Stat(moduleDirPath); os.IsNotExist(err) {
			continue
		}

		// Clean up content inside the module directory
		removed, err := dm.cleanupModuleDirectoryContent(moduleDirPath, module.Version, module.Name)
		if err != nil {
			dm.logger.Warn("Failed to cleanup module content",
				zap.String("module", module.Name),
				zap.String("path", moduleDirPath),
				zap.Error(err))
			continue
		}

		removedContent = append(removedContent, removed...)
	}

	return removedContent
}

// cleanupModuleDirectoryContent cleans up content inside a specific module directory
func (dm *DependencyManager) cleanupModuleDirectoryContent(moduleDirPath, expectedVersion, moduleName string) ([]string, error) {
	removedContent := []string{}

	// Read the module directory
	entries, err := os.ReadDir(moduleDirPath)
	if err != nil {
		return nil, fmt.Errorf("read module directory %s: %w", moduleDirPath, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		entryPath := filepath.Join(moduleDirPath, entry.Name())

		// Check if this directory name contains a version that doesn't match expected version
		if dm.shouldRemoveModuleContent(entry.Name(), expectedVersion) {
			dm.logger.Info("Removing unused module content",
				zap.String("module", moduleName),
				zap.String("path", entryPath),
				zap.String("content", entry.Name()),
				zap.String("expected_version", expectedVersion))

			// Remove the directory
			if err := os.RemoveAll(entryPath); err != nil {
				dm.logger.Error("Failed to remove module content",
					zap.String("path", entryPath),
					zap.Error(err))
				return nil, fmt.Errorf("remove module content %s: %w", entryPath, err)
			}

			removedContent = append(removedContent, entry.Name())
		}
	}

	return removedContent, nil
}

// shouldRemoveModuleContent determines if a module content directory should be removed
func (dm *DependencyManager) shouldRemoveModuleContent(contentName, expectedVersion string) bool {
	// Look for version patterns in the content name
	// Common patterns: module-name-version, module_name_version, module@version

	// Normalize expected version (remove 'v' prefix if present)
	normalizedExpected := expectedVersion
	if strings.HasPrefix(expectedVersion, "v") {
		normalizedExpected = expectedVersion[1:]
	}

	// Check if content name contains a version that's different from expected
	// This is a simple heuristic - you might want to make it more sophisticated

	// If the content name contains a version pattern and it doesn't match expected version
	if strings.Contains(contentName, "-") {
		// Try to extract version from content name
		parts := strings.Split(contentName, "-")
		if len(parts) >= 2 {
			// Check if the last part looks like a version
			lastPart := parts[len(parts)-1]
			if dm.isVersionString(lastPart) && lastPart != normalizedExpected {
				dm.logger.Debug("Found version mismatch in content name",
					zap.String("content_name", contentName),
					zap.String("extracted_version", lastPart),
					zap.String("expected_version", normalizedExpected))
				return true
			}
		}
	}

	// Check for @version pattern
	if strings.Contains(contentName, "@") {
		parts := strings.Split(contentName, "@")
		if len(parts) == 2 {
			version := parts[1]
			if dm.isVersionString(version) && version != normalizedExpected {
				dm.logger.Debug("Found version mismatch in @ pattern",
					zap.String("content_name", contentName),
					zap.String("extracted_version", version),
					zap.String("expected_version", normalizedExpected))
				return true
			}
		}
	}

	return false
}

// isVersionString checks if a string looks like a version number
func (dm *DependencyManager) isVersionString(s string) bool {
	// Simple version pattern matching
	// Look for patterns like: 1.0.0, v1.0.0, 0.0.6, etc.
	if len(s) == 0 {
		return false
	}

	// Remove 'v' prefix if present
	if s[0] == 'v' {
		s = s[1:]
	}

	// Check if it contains dots (semantic versioning)
	if !strings.Contains(s, ".") {
		return false
	}

	// Split by dots and check if all parts are numeric
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}

	for _, part := range parts {
		if len(part) == 0 {
			return false
		}
		// Check if all characters are digits
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}

	return true
}

// CleanupAllUnusedModules removes unused module directories and records statistics
// This is the main cleanup function that should be called after dependency updates
func (dm *DependencyManager) CleanupAllUnusedModules(ctx context.Context, _ string, newLockFile *moduleloader.LockFile, stats *ModuleOperationStats) error {
	// First, clean up unused modules (entire module directories)
	removedModules, err := dm.CleanupUnusedModules(ctx, newLockFile)
	if err != nil {
		return fmt.Errorf("cleanup unused modules: %w", err)
	}

	// Add removed modules to stats
	for moduleName, relPath := range removedModules {
		version := dm.extractVersionFromPath(relPath)
		if version != "unknown" {
			version = "v" + version
		}
		stats.AddRemoved(moduleName, version)
	}

	return nil
}
