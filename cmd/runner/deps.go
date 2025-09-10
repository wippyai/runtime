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
	Name       string
	Version    string
	OldVersion string // Previous version for updates
	Action     string // "installed", "updated", "removed"
}

// OperationStats tracks module operations
type OperationStats struct {
	Installed   int
	Updated     int
	Removed     int
	Modules     []ModuleOperation
	ModuleStats []moduleloader.ModuleStats
}

// DependencyManager handles dependency installation and updates
type DependencyManager struct {
	config         *Config
	logger         *zap.Logger
	registryLoader RegistryLoader
	lockFileMgr    LockFileManager
}

// NewDependencyManager creates a new dependency manager
func NewDependencyManager(config *Config, logger *zap.Logger) *DependencyManager {
	return &DependencyManager{
		config:         config,
		logger:         logger,
		registryLoader: &defaultRegistryLoader{config: config, logger: logger},
		lockFileMgr:    &defaultLockFileManager{logger: logger},
	}
}

// InstallDependencies installs dependencies from lock file
func (dm *DependencyManager) InstallDependencies(ctx context.Context) error {
	dm.logger.Info(LogInstallingDependencies)

	// Load lock file
	lockFile, _, err := dm.loadLockFile()
	if err != nil {
		return err
	}

	// Show lock file operations header (like update command)
	dm.logger.Info("Lock file operations: 0 installs, 0 updates, 0 removals:")

	// Load and process dependencies
	loadResult, err := dm.loadAndProcessDependencies(ctx, lockFile.Directories.Src)
	if err != nil {
		return err
	}

	// Display module statistics from Load() if available
	if len(loadResult.ModuleStats) > 0 {
		dm.displayModuleStatistics(loadResult.ModuleStats)
	}

	dm.logger.Info(LogInstallationCompleted)
	return nil
}

// InstallDependenciesSilent installs dependencies from lock file without detailed output
// This is used by the update command to avoid duplicate output
// Returns statistics about installed modules
func (dm *DependencyManager) InstallDependenciesSilent(ctx context.Context) (*OperationStats, error) {
	// Determine lock file path
	lockPath, err := moduleloader.FindLockFile(dm.config.FolderPath, dm.config.LockFile)
	if err != nil {
		return nil, fmt.Errorf("find lock file: %w", err)
	}

	// Check if lock file exists
	if lockPath == "" {
		return nil, fmt.Errorf("no lock file found in project directory: %s", dm.config.FolderPath)
	}

	// Load lock file
	lockFile, err := moduleloader.LoadLockFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("load lock file: %w", err)
	}

	// Install dependencies using the method that shows status
	stats, err := dm.installModulesFromLockFileSilent(ctx, lockFile, lockPath)
	if err != nil {
		return nil, fmt.Errorf("install modules: %w", err)
	}

	return stats, nil
}

// UpdateDependencies updates dependencies and regenerates lock file
func (dm *DependencyManager) UpdateDependencies(ctx context.Context) error {
	dm.logger.Info(LogUpdatingDependencies)

	// Load existing lock file to compare changes
	existingLock, srcDir, modulesDir, existingReplacements := dm.loadExistingLockFile()

	// Load and process dependencies
	loadResult, err := dm.loadAndProcessDependencies(ctx, srcDir)
	if err != nil {
		return err
	}

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

	// Display module statistics from Load() if available
	if len(loadResult.ModuleStats) > 0 {
		dm.displayModuleStatistics(loadResult.ModuleStats)
	}

	return nil
}

// displayModuleStatistics displays module statistics in the requested format
func (dm *DependencyManager) displayModuleStatistics(stats []moduleloader.ModuleStats) {
	if len(stats) == 0 {
		return
	}

	// Count operations by type
	installs := 0
	updates := 0
	removals := 0

	for _, stat := range stats {
		switch stat.Status {
		case StatusDownloaded, StatusFromReplacement:
			installs++
		case StatusFromCache, StatusSkipped:
			// These don't count as operations
		}
	}

	dm.logger.Info(LogInstallingDependencies)
	dm.logger.Info(fmt.Sprintf(LogPackageOperations,
		installs, updates, removals))

	// Display each module with its status
	for _, stat := range stats {
		var statusText string
		switch stat.Status {
		case StatusFromCache:
			statusText = fmt.Sprintf(" - Skipping %s: %s", stat.Name, stat.Version)
		case StatusDownloaded:
			statusText = fmt.Sprintf(" - Downloading %s: %s", stat.Name, stat.Version)
		case StatusFromReplacement:
			statusText = fmt.Sprintf(" - Installing %s: %s (from replacement)", stat.Name, stat.Version)
		case StatusSkipped:
			statusText = fmt.Sprintf(" - Skipping %s: %s", stat.Name, stat.Version)
		default:
			statusText = fmt.Sprintf(" - Installing %s: %s", stat.Name, stat.Version)
		}
		dm.logger.Info(statusText)
	}
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

// installModulesFromLockFileSilent installs modules from a lock file without detailed output
// This is used by the update command to avoid duplicate output
func (dm *DependencyManager) installModulesFromLockFileSilent(ctx context.Context, lockFile *moduleloader.LockFile, lockPath string) (*OperationStats, error) {
	if len(lockFile.Modules) == 0 {
		return &OperationStats{}, nil
	}

	// Create a map of replacements for quick lookup
	replacements := make(map[string]string)
	for _, replacement := range lockFile.Replacements {
		replacements[replacement.From] = replacement.To
	}

	// Track operations
	stats := &OperationStats{}

	// Install each module silently
	for _, module := range lockFile.Modules {
		dm.logger.Debug("Processing module for installation",
			zap.String("name", module.Name),
			zap.String("version", module.Version))

		// Check if module is already installed to determine if it's an update
		wasInstalled, oldVersion := dm.isModuleInstalledFromLockFile(module, lockFile)

		// Install module with status output
		actuallyInstalled, err := dm.installModuleFromLockFileWithStatus(ctx, module, replacements, lockPath, lockFile)
		if err != nil {
			return nil, fmt.Errorf("install module %s: %w", module.Name, err)
		}

		// Only record the operation if module was actually installed
		if actuallyInstalled {
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
				Name:       module.Name,
				Version:    module.Version,
				OldVersion: oldVersion,
				Action:     action,
			})
		}
	}

	return stats, nil
}

// isModuleInstalledFromLockFile checks if a module is already installed based on lock file
// Returns true if installed and the old version string
func (dm *DependencyManager) isModuleInstalledFromLockFile(module moduleloader.LockedModule, lockFile *moduleloader.LockFile) (bool, string) {
	// Check if any module directory exists for this module
	moduleBaseDir := filepath.Join(lockFile.Directories.Modules, module.Name)
	if _, err := os.Stat(moduleBaseDir); err != nil {
		return false, ""
	}

	// Look for module directories with commit hash pattern
	entries, err := os.ReadDir(moduleBaseDir)
	if err != nil {
		return false, ""
	}

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), module.Name+"@") {
			// Extract version from directory name (format: module@version)
			dirName := entry.Name()
			if atIndex := strings.LastIndex(dirName, "@"); atIndex != -1 {
				oldVersion := dirName[atIndex+1:]
				return true, oldVersion
			}
			return true, "unknown"
		}
	}

	return false, ""
}

// installModuleFromLockFileWithStatus installs a single module from lock file with status reporting
// Returns true if module was actually installed, false if it was skipped
func (dm *DependencyManager) installModuleFromLockFileWithStatus(ctx context.Context, module moduleloader.LockedModule, replacements map[string]string, _ string, lockFile *moduleloader.LockFile) (bool, error) {
	dm.logger.Debug("Installing module from lock file",
		zap.String("name", module.Name),
		zap.String("version", module.Version))

	// Check if this module has a replacement
	if _, hasReplacement := replacements[module.Name]; hasReplacement {
		// Use the custom path from replacement
		// dm.logger.Info(fmt.Sprintf(" - Installing %s: %s (from replacement)", module.Name, module.Version))
		return true, nil
	}

	// Parse the module name to get organization and module parts
	name, err := moduleloader.ParseName(module.Name)
	if err != nil {
		return false, fmt.Errorf("invalid module name %s: %w", module.Name, err)
	}

	// Check if module is already installed but with different version (skip)
	if wasInstalled, _ := dm.isModuleInstalledFromLockFile(module, lockFile); wasInstalled {
		dm.logger.Info(fmt.Sprintf(" - Skipping %s: %s", module.Name, module.Version))
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
		dm.logger.Info(fmt.Sprintf("     - %s %s: %s => %s", action, module.Name, module.OldVersion, module.Version))
	}

	// Display removed modules
	for _, module := range changes.Removed {
		dm.logger.Info(fmt.Sprintf("     - Removing %s", module.Name))
	}

	// Display installed modules
	for _, module := range changes.Installed {
		dm.logger.Info(fmt.Sprintf("     - Installing %s: %s", module.Name, module.Version))
	}
}

// isVersionDowngrade determines if going from oldVersion to newVersion is a downgrade
func (dm *DependencyManager) isVersionDowngrade(oldVersion, newVersion string) bool {
	// Simple version comparison - assumes semantic versioning
	// This is a basic implementation; for production use, you might want to use a proper semver library
	return oldVersion > newVersion
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
