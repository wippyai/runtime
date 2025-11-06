package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"

	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/deps"
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

// DependencyManager handles dependency installation and updates
type DependencyManager struct {
	folderPath string
	lockFile   string
	logger     *zap.Logger
}

// NewDependencyManager creates a new dependency manager
func NewDependencyManager(folderPath, lockFile string, logger *zap.Logger) *DependencyManager {
	return &DependencyManager{
		folderPath: folderPath,
		lockFile:   lockFile,
		logger:     logger,
	}
}

// InstallDependencies installs dependencies from lock file
func (dm *DependencyManager) InstallDependencies(ctx context.Context) error {
	lockPath, err := deps.FindLockFile(dm.folderPath, dm.lockFile)
	if err != nil {
		return fmt.Errorf("find lock file: %w", err)
	}

	if lockPath == "" {
		return fmt.Errorf("no lock file found in project directory: %s", dm.folderPath)
	}

	lockFile, err := deps.LoadLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("load lock file: %w", err)
	}

	if err := dm.installModulesFromLockFile(ctx, lockFile, lockPath); err != nil {
		return fmt.Errorf("install modules: %w", err)
	}

	return nil
}

// InstallDependenciesFromLockFile installs dependencies from an already loaded lock file
func (dm *DependencyManager) InstallDependenciesFromLockFile(ctx context.Context, lockFile *deps.LockFile, lockPath string) error {
	if err := dm.installModulesFromLockFile(ctx, lockFile, lockPath); err != nil {
		return fmt.Errorf("install modules: %w", err)
	}
	return nil
}

// UpdateDependencies updates dependencies and regenerates lock file
func (dm *DependencyManager) UpdateDependencies(ctx context.Context) error {
	var srcDir, modulesDir string
	var excludeDirs []string

	existingLockPath := filepath.Join(dm.folderPath, dm.lockFile)
	var existingLock *deps.LockFile
	if lock, err := deps.LoadLockFile(existingLockPath); err == nil {
		existingLock = lock
		srcDir = existingLock.Directories.Src
		modulesDir = existingLock.Directories.Modules
	} else {
		srcDir = "."
		modulesDir = deps.WippyFolder
	}

	// Prepare list of directories to exclude from source scanning
	excludeDirs = dm.prepareExcludeDirs(srcDir, modulesDir, existingLock, existingLockPath)

	entries, err := dm.loadRegistryEntries(ctx, srcDir, excludeDirs)
	if err != nil {
		return fmt.Errorf("load registry entries: %w", err)
	}

	// Load existing replacements from lock file to avoid re-downloading local modules
	var existingReplacements []deps.Replacement
	if existingLock, err := deps.LoadLockFile(existingLockPath); err == nil {
		existingReplacements = existingLock.Replacements
		if len(existingReplacements) > 0 {
			dm.logger.Debug("Found replacements in existing lock file",
				zap.Int("count", len(existingReplacements)))
		}
	}

	// Create a temporary directory for dependency resolution during update
	tempDir := filepath.Join(dm.folderPath, deps.TempUpdateDir)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("create temporary directory: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			dm.logger.Warn("Failed to cleanup temporary directory",
				zap.String("path", tempDir),
				zap.Error(cleanupErr))
		}
	}()

	dm.logger.Debug("Using temporary directory for dependency resolution",
		zap.String("temp_dir", tempDir))

	// Use temporary directory for dependency resolution with replacements support
	registryLoader := dm.createModuleLoaderManagerWithTempDirAndReplacements(entries, tempDir, existingReplacements, existingLockPath)
	loadResult, err := registryLoader.Load(ctx)
	if err != nil {
		return fmt.Errorf("load dependencies: %w", err)
	}

	lockFile := deps.ConvertToLockFile(loadResult, modulesDir, srcDir)
	if len(existingReplacements) > 0 {
		lockFile.Replacements = existingReplacements
	}

	lockPath := filepath.Join(dm.folderPath, dm.lockFile)
	if err := lockFile.SaveLockFile(lockPath); err != nil {
		return fmt.Errorf("save lock file: %w", err)
	}

	return nil
}

// CleanUnusedPackages removes packages not in lock file
func (dm *DependencyManager) CleanUnusedPackages(lockFile *deps.LockFile) error {
	vendorPath := lockFile.GetModulesVendorPath()
	modulesDir := filepath.Join(dm.folderPath, vendorPath)

	if _, err := os.Stat(modulesDir); os.IsNotExist(err) {
		return nil
	}

	expectedPackages := make(map[string]bool)
	for _, module := range lockFile.Modules {
		expectedPackages[module.Name] = true
	}

	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return fmt.Errorf("failed to read modules directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if !expectedPackages[entry.Name()] {
			packagePath := filepath.Join(modulesDir, entry.Name())
			if err := os.RemoveAll(packagePath); err != nil {
				dm.logger.Warn("Failed to remove unused package",
					zap.String("package", entry.Name()),
					zap.Error(err))
			}
		}
	}

	return nil
}

func (dm *DependencyManager) loadRegistryEntries(ctx context.Context, srcDir string, excludeDirs []string) ([]regapi.Entry, error) {
	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	folderLoader := loader.NewLoader(dtt, dm.logger, interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadFile),
	))

	srcPath := filepath.Join(dm.folderPath, srcDir)
	var fsys = os.DirFS(srcPath)

	// Apply filtering if we have directories to exclude
	if len(excludeDirs) > 0 {
		fsys = newFilteredFS(fsys, excludeDirs)
	}

	entries, err := folderLoader.LoadFS(ctx, fsys)
	if err != nil {
		return nil, fmt.Errorf("load entries from directory %s: %w", srcPath, err)
	}

	return entries, nil
}

// prepareExcludeDirs prepares a list of directories to exclude from source scanning.
//
// It calculates relative paths (relative to srcDir) for:
//   - modules directory (if inside source directory)
//   - replacement directories from lock file (if inside source directory)
//
// Parameters:
//   - srcDir: source directory path (relative to dm.folderPath)
//   - modulesDir: modules directory path (relative to dm.folderPath)
//   - lockFile: parsed lock file (may be nil)
//   - lockPath: path to the lock file
//
// Returns a list of relative paths (relative to srcDir) to exclude.
// Paths outside the source directory or paths that fail validation are
// silently skipped with debug logging.
//
// Example:
//
//	srcDir = "."
//	modulesDir = ".wippy"
//	→ returns [".wippy"]
func (dm *DependencyManager) prepareExcludeDirs(srcDir, modulesDir string, lockFile *deps.LockFile, lockPath string) []string {
	// Validate inputs
	if srcDir == "" {
		dm.logger.Warn("prepareExcludeDirs called with empty srcDir, using '.' as default")
		srcDir = "."
	}

	var excludeDirs []string

	// Exclude modules directory if it's inside source directory
	if modulesDir != "" {
		absModulesPath := filepath.Join(dm.folderPath, modulesDir)
		absSrcPath := filepath.Join(dm.folderPath, srcDir)

		relModulesPath, err := filepath.Rel(absSrcPath, absModulesPath)
		if err != nil {
			dm.logger.Debug("Failed to calculate relative path for modules directory",
				zap.String("src_dir", srcDir),
				zap.String("modules_dir", modulesDir),
				zap.Error(err))
		} else if !strings.HasPrefix(relModulesPath, "..") {
			excludeDirs = append(excludeDirs, relModulesPath)
			dm.logger.Debug("Filtering out modules directory from source scanning",
				zap.String("src_dir", srcDir),
				zap.String("modules_dir", modulesDir),
				zap.String("relative_path", relModulesPath))
		}
	}

	// Exclude replacement directories if they are inside source directory
	if lockFile != nil && len(lockFile.Replacements) > 0 {
		lockFileDir := filepath.Dir(lockPath)
		absSrcPath := filepath.Join(dm.folderPath, srcDir)

		for _, replacement := range lockFile.Replacements {
			absReplacementPath := filepath.Join(lockFileDir, replacement.To)
			relReplacementPath, err := filepath.Rel(absSrcPath, absReplacementPath)
			if err != nil {
				dm.logger.Debug("Failed to calculate relative path for replacement",
					zap.String("module", replacement.From),
					zap.String("replacement_path", replacement.To),
					zap.Error(err))
				continue
			}

			if !strings.HasPrefix(relReplacementPath, "..") {
				excludeDirs = append(excludeDirs, relReplacementPath)
				dm.logger.Debug("Filtering out replacement directory from source scanning",
					zap.String("module", replacement.From),
					zap.String("replacement_path", replacement.To),
					zap.String("relative_path", relReplacementPath))
			}
		}
	}

	return excludeDirs
}

// createModuleLoaderManagerWithTempDirAndReplacements creates a module loader manager
// that uses a temporary directory and applies replacements from lock file
func (dm *DependencyManager) createModuleLoaderManagerWithTempDirAndReplacements(
	entries []regapi.Entry,
	tempDir string,
	replacements []deps.Replacement,
	lockFilePath string,
) *deps.Manager {
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

	// Create entry loader with replacements support
	registryLoader := deps.NewEntryLoaderWithReplacements(entries, replacements, lockFilePath, dm.logger)

	tempVendorFolder := filepath.Join(tempDir, "vendor")
	return deps.NewManager(
		organizationClient,
		moduleClient,
		commitClient,
		labelClient,
		downloadClient,
		registryLoader,
		dm.logger,
		tempVendorFolder,
	)
}

func (dm *DependencyManager) installModulesFromLockFile(ctx context.Context, lockFile *deps.LockFile, lockPath string) error {
	if len(lockFile.Modules) == 0 {
		return nil
	}

	replacements := make(map[string]string)
	for _, replacement := range lockFile.Replacements {
		replacements[replacement.From] = replacement.To
	}

	for _, module := range lockFile.Modules {
		if err := dm.installModuleFromLockFile(ctx, module, replacements, lockPath); err != nil {
			return fmt.Errorf("install module %s: %w", module.Name, err)
		}
	}

	return nil
}

func (dm *DependencyManager) installModuleFromLockFile(ctx context.Context, module deps.LockedModule, replacements map[string]string, lockPath string) error {
	if customPath, hasReplacement := replacements[module.Name]; hasReplacement {
		resolvedPath := filepath.Join(filepath.Dir(lockPath), customPath)
		if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
			return fmt.Errorf("replacement path does not exist: %s (resolved to: %s)", customPath, resolvedPath)
		}
		return nil
	}

	name, err := deps.ParseName(module.Name)
	if err != nil {
		return fmt.Errorf("invalid module name %s: %w", module.Name, err)
	}

	lockFile, err := deps.LoadLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("failed to load lock file to get modules directory: %w", err)
	}

	// Use vendor path (modules + "/vendor")
	vendorPath := lockFile.GetModulesVendorPath()
	modulesDir := filepath.Join(filepath.Dir(lockPath), vendorPath)
	moduleBaseDir := filepath.Join(modulesDir, name.Organization)
	expectedDirName := name.Module + "@" + module.Hash
	expectedModulePath := filepath.Join(moduleBaseDir, expectedDirName)

	// Check if exact hash directory exists and has content
	if stat, err := os.Stat(expectedModulePath); err == nil && stat.IsDir() {
		// Check if it has a module- subdirectory
		subEntries, err := os.ReadDir(expectedModulePath)
		if err == nil && len(subEntries) > 0 {
			// Module with exact hash is already installed and has content
			return nil
		}
	}

	manifest := &deps.Manifest{
		Dependencies: []deps.ManifestDependency{
			{
				Name:    name,
				Version: module.Version,
			},
		},
	}

	loader := &singleModuleLoader{manifest: manifest}

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

	// Use vendor path (modules + "/vendor")
	moduleVendorPath := lockFile.GetModulesVendorPath()
	registryLoader := deps.NewManager(
		organizationClient,
		moduleClient,
		commitClient,
		labelClient,
		downloadClient,
		loader,
		dm.logger,
		moduleVendorPath,
	)

	loadResult, err := registryLoader.Load(ctx)
	if err != nil {
		return fmt.Errorf("install module from lock file %s: %w", module.Name, err)
	}

	if len(loadResult.Modules) == 0 {
		return fmt.Errorf("no modules loaded for %s", module.Name)
	}

	return nil
}

// ShowResults displays the operation results in a formatted way
// This is the new method that replaces DisplayModuleStatistics
func (dm *DependencyManager) ShowResults(stats *deps.ModuleOperationStats) {
	if !stats.HasOperations() {
		return
	}

	// Display operation summary using the same format as LogPackageOperations
	dm.logger.Info(fmt.Sprintf(deps.LogPackageOperations,
		stats.Installed, stats.Updated, stats.Removed))

	// Display detailed operations from our new methods
	for _, op := range stats.Operations {
		var statusText string
		switch op.Action {
		case deps.ActionInstalled:
			statusText = fmt.Sprintf(" - Installing %s: %s", op.Name, op.Version)
		case deps.ActionUpdated:
			statusText = fmt.Sprintf(" - Updating %s: %s → %s", op.Name, op.OldVersion, op.Version)
		case deps.ActionRemoved:
			statusText = fmt.Sprintf(" - Removing %s: %s", op.Name, op.Version)
		case deps.ActionSkipped:
			// Only show Skipping messages in verbose mode
			if stats.Verbose {
				statusText = fmt.Sprintf(" - Skipping %s: %s", op.Name, op.Version)
			} else {
				continue
			}
		default:
			continue
		}
		dm.logger.Info(statusText)
	}

	// Display module operations from moduleloader
	for _, stat := range stats.ModuleStats {
		dm.displayModuleOperation(stat, stats.Verbose)
	}

	// Removed modules are now displayed through Operations above
}

// displayModuleOperation displays a single module operation with appropriate status message
func (dm *DependencyManager) displayModuleOperation(stat deps.ModuleStats, verbose bool) {
	var statusText string

	switch stat.Status {
	case deps.StatusFromCache:
		// Only show Skipping messages in verbose mode
		if verbose {
			statusText = fmt.Sprintf(" - Skipping %s: %s", stat.Name, stat.Version)
		} else {
			return
		}
	case deps.StatusDownloaded:
		statusText = fmt.Sprintf(" - Downloading %s: %s", stat.Name, stat.Version)
	case deps.StatusFromReplacement:
		statusText = fmt.Sprintf(" - Using %s: %s (from replacement)", stat.Name, stat.Version)
	case deps.StatusSkipped:
		// Only show Skipping messages in verbose mode
		if verbose {
			statusText = fmt.Sprintf(" - Skipping %s: %s", stat.Name, stat.Version)
		} else {
			return
		}
	default:
		// Skip unknown statuses (including StatusRemoved to avoid duplication)
		return
	}

	dm.logger.Info(statusText)
}

// CleanupModuleContent removes unused content from module directories
// This function looks inside module directories and removes subdirectories
// that don't match the expected version from the lock file
func (dm *DependencyManager) CleanupModuleContent(_ context.Context, newLockFile *deps.LockFile) []string {
	// Get the vendor directory from the new lock file (modules + "/vendor")
	vendorPath := newLockFile.GetModulesVendorPath()

	// Resolve the full path to the modules directory
	modulesPath := filepath.Join(dm.folderPath, vendorPath)

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
		name, err := deps.ParseName(module.Name)
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
			dm.logger.Debug(fmt.Sprintf(" - Removing unused content for %s: %s", moduleName, entry.Name()))

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
func (dm *DependencyManager) CleanupAllUnusedModules(ctx context.Context, _ string, newLockFile *deps.LockFile, stats *deps.ModuleOperationStats) error {
	// First, clean up unused modules (entire module directories)
	removedModules, err := dm.CleanupUnusedModules(ctx, newLockFile)
	if err != nil {
		return fmt.Errorf("cleanup unused modules: %w", err)
	}

	// Add removed modules to stats
	for moduleName, relPath := range removedModules {
		// Extract version from module folder name (last part of path should be module folder like "module-xxx-v1.2.3")
		moduleFolderName := filepath.Base(relPath)
		var version string
		if moduleFolderName != "" {
			version = dm.ExtractVersionFromModuleFolder(moduleFolderName)
		}
		if version == "" {
			version = "unknown"
		}
		stats.AddRemoved(moduleName, version)
	}

	return nil
}

// CleanupUnusedModules removes module directories that are not listed in the new lock file
// Returns a list of removed module names for further use
func (dm *DependencyManager) CleanupUnusedModules(_ context.Context, newLockFile *deps.LockFile) (map[string]string, error) {
	// Get the vendor directory from the new lock file (modules + "/vendor")
	vendorPath := newLockFile.GetModulesVendorPath()

	// Resolve the full path to the modules directory
	modulesPath := filepath.Join(dm.folderPath, vendorPath)

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
		name, err := deps.ParseName(module.Name)
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
		replacementPath := filepath.Join(dm.folderPath, replacement.To)
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
				// Try to extract module name from the directory path (format: org/module or org/module@hash)
				moduleName := orgEntry.Name() + "/" + strings.Split(moduleEntry.Name(), "@")[0]

				// Look for module subdirectories inside this directory
				moduleSubEntries, err := os.ReadDir(modulePath)
				if err == nil && len(moduleSubEntries) > 0 {
					// Find the actual module subdirectory
					for _, subEntry := range moduleSubEntries {
						if subEntry.IsDir() && strings.HasPrefix(subEntry.Name(), "module-") {
							moduleSubRelPath := filepath.Join(relPath, subEntry.Name())
							// Use the subdirectory path for better version extraction
							removedModules[moduleName] = moduleSubRelPath
							break
						}
					}
					// If no module- subdirectory found, still add to removedModules but with the directory path
					if _, exists := removedModules[moduleName]; !exists {
						removedModules[moduleName] = relPath
					}
				} else {
					// No subdirectories, just use the directory path
					removedModules[moduleName] = relPath
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

// ExtractModuleNameFromPath extracts a module name from a directory path
// Handles formats like "org/module" and "org/module@version"
func (dm *DependencyManager) ExtractModuleNameFromPath(relPath string) string {
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

// isModuleInstalledFromLockFile checks if a module is already installed based on lock file
// Returns true if installed and the old version string
func (dm *DependencyManager) IsModuleInstalledFromLockFile(module deps.LockedModule, lockFile *deps.LockFile) (bool, string) {
	// Parse module name to get organization and module parts
	name, err := deps.ParseName(module.Name)
	if err != nil {
		dm.logger.Debug("Failed to parse module name", zap.Error(err))
		return false, ""
	}

	// Check if any module directory exists for this module
	// Structure: .wippy/vendor/organization/ (contains module@hash directories)
	vendorPath := lockFile.GetModulesVendorPath()
	organizationDir := filepath.Join(vendorPath, name.Organization)

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

// IsVersionDowngrade determines if going from oldVersion to newVersion is a downgrade
func (dm *DependencyManager) IsVersionDowngrade(oldVersion, newVersion string) bool {
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

// InstalledPackageState represents a single installed package with its location and version
type InstalledPackageState struct {
	Name         string // Module name in "org/module" format
	Version      string // Version string
	Hash         string // Commit hash
	ModuleFolder string // The actual module folder name (e.g., "module-wippy-security-v0.3.0")
	RelPath      string // Relative path from vendor directory
}

// ScanInstalledPackages scans the filesystem and returns a map of currently installed packages
func (dm *DependencyManager) ScanInstalledPackages(lockFile *deps.LockFile) map[string]*InstalledPackageState {
	// Get the vendor directory from the lock file
	vendorPath := lockFile.GetModulesVendorPath()
	modulesPath := filepath.Join(dm.folderPath, vendorPath)

	installedPackages := make(map[string]*InstalledPackageState)

	// Check if modules directory exists
	if _, err := os.Stat(modulesPath); os.IsNotExist(err) {
		dm.logger.Debug("Modules directory does not exist, nothing to scan",
			zap.String("path", modulesPath))
		return installedPackages
	}

	// Scan all organization directories
	orgEntries, err := os.ReadDir(modulesPath)
	if err != nil {
		dm.logger.Warn("Failed to read modules directory",
			zap.String("path", modulesPath),
			zap.Error(err))
		return installedPackages
	}

	for _, orgEntry := range orgEntries {
		if !orgEntry.IsDir() {
			continue
		}

		orgPath := filepath.Join(modulesPath, orgEntry.Name())

		// Read all module@hash directories within this organization
		moduleEntries, err := os.ReadDir(orgPath)
		if err != nil {
			dm.logger.Debug("Failed to read organization directory",
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

			// Extract module name and hash from directory name (format: module@hash)
			var moduleName string
			var hash string
			dirName := moduleEntry.Name()
			if atIndex := strings.LastIndex(dirName, "@"); atIndex != -1 {
				moduleName = dirName[:atIndex]
				hash = dirName[atIndex+1:]
			} else {
				// Skip directories without hash
				continue
			}

			// Read module subdirectories
			moduleSubEntries, err := os.ReadDir(modulePath)
			if err != nil || len(moduleSubEntries) == 0 {
				continue
			}

			// Find the module folder with "module-" prefix
			for _, subEntry := range moduleSubEntries {
				if subEntry.IsDir() && strings.HasPrefix(subEntry.Name(), "module-") {
					// Extract version from the module folder name
					version := dm.ExtractVersionFromModuleFolder(subEntry.Name())
					if version != "" {
						packageName := fmt.Sprintf("%s/%s", orgEntry.Name(), moduleName)
						installedPackages[packageName] = &InstalledPackageState{
							Name:         packageName,
							Version:      version,
							Hash:         hash,
							ModuleFolder: subEntry.Name(),
							RelPath:      filepath.Join(relPath, subEntry.Name()),
						}
					}
					break
				}
			}
		}
	}

	return installedPackages
}

// ExtractVersionFromModuleFolder extracts the version from a module folder name
// Example: "module-wippy-security-v0.3.0" -> "v0.3.0"
func (dm *DependencyManager) ExtractVersionFromModuleFolder(folderName string) string {
	// Remove 'module-' prefix if present
	withoutPrefix := folderName
	if strings.HasPrefix(folderName, "module-") {
		withoutPrefix = folderName[7:]
	}

	// Find the last occurrence of a dash followed by a version (with or without 'v' prefix)
	for i := len(withoutPrefix) - 1; i >= 0; i-- {
		if withoutPrefix[i] == '-' {
			potentialVersion := withoutPrefix[i+1:]
			// Version with 'v' prefix
			if len(potentialVersion) > 1 && potentialVersion[0] == 'v' && potentialVersion[1] >= '0' && potentialVersion[1] <= '9' {
				if strings.Contains(potentialVersion[1:], ".") {
					return potentialVersion
				}
			}
			// Version without 'v' prefix
			if len(potentialVersion) > 0 && potentialVersion[0] >= '0' && potentialVersion[0] <= '9' {
				if strings.Contains(potentialVersion, ".") {
					return potentialVersion
				}
			}
		}
	}

	return ""
}

// ComparePackageStates compares two package states and returns differences
func ComparePackageStates(oldState, newState map[string]*InstalledPackageState) *deps.ModuleOperationStats {
	stats := deps.NewModuleOperationStats(true)

	// Find newly installed packages
	for name, newPkg := range newState {
		if _, exists := oldState[name]; !exists {
			// This is a new installation
			stats.AddInstalled(name, newPkg.Version)
		} else {
			// This package exists in both states - check if it's updated
			oldPkg := oldState[name]
			if oldPkg.Hash != newPkg.Hash || oldPkg.Version != newPkg.Version {
				stats.AddUpdated(name, newPkg.Version, oldPkg.Version)
			}
		}
	}

	// Find removed packages
	for name, oldPkg := range oldState {
		if _, exists := newState[name]; !exists {
			stats.AddRemoved(name, oldPkg.Version)
		}
	}

	return stats
}

// CleanupGarbageDirectories removes directories from vendor that don't match expected module structure
func (dm *DependencyManager) CleanupGarbageDirectories(lockFile *deps.LockFile) ([]string, error) {
	// Get the vendor directory
	vendorPath := lockFile.GetModulesVendorPath()
	modulesPath := filepath.Join(dm.folderPath, vendorPath)

	// Check if modules directory exists
	if _, err := os.Stat(modulesPath); os.IsNotExist(err) {
		dm.logger.Debug("Vendor directory does not exist, nothing to clean",
			zap.String("path", modulesPath))
		return []string{}, nil
	}

	// Build a map of expected organizations and their modules from lock file
	expectedModules := make(map[string]map[string]bool)       // org -> set of module@hash
	modulesByOrgAndName := make(map[string]map[string]string) // org/moduleName -> hash

	for _, module := range lockFile.Modules {
		name, err := deps.ParseName(module.Name)
		if err != nil {
			continue
		}

		if expectedModules[name.Organization] == nil {
			expectedModules[name.Organization] = make(map[string]bool)
		}

		// Store the expected hash for this module
		if modulesByOrgAndName[name.Organization] == nil {
			modulesByOrgAndName[name.Organization] = make(map[string]string)
		}
		modulesByOrgAndName[name.Organization][name.Module] = module.Hash

		// Mark expected module with hash
		if module.Hash != "" {
			expectedModules[name.Organization][name.Module+"@"+module.Hash] = true
		} else {
			expectedModules[name.Organization][name.Module] = true
		}
	}

	removedDirs := []string{}

	// Scan all organizations in vendor
	orgEntries, err := os.ReadDir(modulesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read vendor directory: %w", err)
	}

	for _, orgEntry := range orgEntries {
		if !orgEntry.IsDir() {
			continue
		}

		orgName := orgEntry.Name()
		orgPath := filepath.Join(modulesPath, orgName)

		// Check if this organization is expected
		expectedModulesForOrg, orgExists := expectedModules[orgName]
		if !orgExists {
			// Organization not in lock file - remove entire directory
			dm.logger.Info("Removing unknown organization directory",
				zap.String("org", orgName),
				zap.String("path", orgPath))
			if err := os.RemoveAll(orgPath); err != nil {
				dm.logger.Warn("Failed to remove organization directory",
					zap.String("org", orgName),
					zap.Error(err))
			} else {
				removedDirs = append(removedDirs, orgPath)
			}
			continue
		}

		// Scan modules within this organization
		moduleEntries, err := os.ReadDir(orgPath)
		if err != nil {
			dm.logger.Debug("Failed to read organization directory",
				zap.String("org", orgName),
				zap.Error(err))
			continue
		}

		for _, moduleEntry := range moduleEntries {
			if !moduleEntry.IsDir() {
				continue
			}

			moduleDirName := moduleEntry.Name()
			modulePath := filepath.Join(orgPath, moduleDirName)

			// Parse module name and hash from directory name
			moduleName := moduleDirName
			var installedHash string
			if atIndex := strings.LastIndex(moduleDirName, "@"); atIndex != -1 {
				moduleName = moduleDirName[:atIndex]
				installedHash = moduleDirName[atIndex+1:]
			}

			// Check if this module is expected from lock file
			expectedHash, moduleExists := modulesByOrgAndName[orgName][moduleName]
			isExpected := false

			if moduleExists && expectedHash != "" {
				// Module exists in lock file with hash
				// It's expected ONLY if the installed hash matches the expected hash
				installedHashWithPrefix := moduleDirName
				isExpected = (installedHash == expectedHash) ||
					strings.HasSuffix(installedHashWithPrefix, "@"+expectedHash)
			} else if moduleExists {
				// Module exists in lock file but no hash specified
				isExpected = !strings.Contains(moduleDirName, "@")
			}

			// Also check if the directory name exactly matches one in expected modules
			if !isExpected && expectedModulesForOrg[moduleDirName] {
				isExpected = true
			}

			if !isExpected {
				// Module with this hash is not in lock file - remove it
				dm.logger.Debug("Removing outdated module directory",
					zap.String("org", orgName),
					zap.String("module", moduleDirName),
					zap.String("installed_hash", installedHash),
					zap.String("expected_hash", expectedHash))
				if err := os.RemoveAll(modulePath); err != nil {
					dm.logger.Warn("Failed to remove module directory",
						zap.String("path", modulePath),
						zap.Error(err))
				} else {
					removedDirs = append(removedDirs, modulePath)
				}
			}
		}

		// Check if organization directory is now empty
		remainingEntries, err := os.ReadDir(orgPath)
		if err == nil && len(remainingEntries) == 0 {
			dm.logger.Debug("Removing empty organization directory",
				zap.String("org", orgName))
			if err := os.Remove(orgPath); err != nil {
				dm.logger.Debug("Failed to remove empty organization directory",
					zap.String("path", orgPath),
					zap.Error(err))
			} else {
				removedDirs = append(removedDirs, orgPath)
			}
		}
	}

	return removedDirs, nil
}

type singleModuleLoader struct {
	manifest *deps.Manifest
}

func (l *singleModuleLoader) LoadManifest(_ context.Context) (*deps.Manifest, error) {
	return l.manifest, nil
}
