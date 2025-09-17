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

// UpdateDependencies updates dependencies and regenerates lock file
func (dm *DependencyManager) UpdateDependencies(ctx context.Context) error {
	var srcDir string
	existingLockPath := filepath.Join(dm.folderPath, dm.lockFile)
	if existingLock, err := deps.LoadLockFile(existingLockPath); err == nil {
		srcDir = existingLock.Directories.Src
	} else {
		srcDir = "."
	}

	entries, err := dm.loadRegistryEntries(ctx, srcDir)
	if err != nil {
		return fmt.Errorf("load registry entries: %w", err)
	}

	registryLoader := dm.createModuleLoaderManager(entries)
	loadResult, err := registryLoader.Load(ctx)
	if err != nil {
		return fmt.Errorf("load dependencies: %w", err)
	}

	if err := dm.installModules(ctx, loadResult); err != nil {
		return fmt.Errorf("install modules: %w", err)
	}

	var modulesDir string
	var existingReplacements []deps.Replacement
	if existingLock, err := deps.LoadLockFile(existingLockPath); err == nil {
		modulesDir = existingLock.Directories.Modules
		existingReplacements = existingLock.Replacements
	} else {
		modulesDir = ".wippy"
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
	modulesDir := filepath.Join(dm.folderPath, lockFile.Directories.Modules)

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

func (dm *DependencyManager) loadRegistryEntries(ctx context.Context, srcDir string) ([]regapi.Entry, error) {
	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	folderLoader := loader.NewLoader(dtt, dm.logger, interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadFile),
	))

	srcPath := filepath.Join(dm.folderPath, srcDir)
	fsys := os.DirFS(srcPath)

	entries, err := folderLoader.LoadFS(ctx, fsys)
	if err != nil {
		return nil, fmt.Errorf("load entries from directory %s: %w", srcPath, err)
	}

	return entries, nil
}

func (dm *DependencyManager) createModuleLoaderManager(entries []regapi.Entry) *deps.Manager {
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

	registryLoader := deps.NewEntryLoader(entries, dm.logger)

	return deps.NewManager(
		organizationClient,
		moduleClient,
		commitClient,
		labelClient,
		downloadClient,
		registryLoader,
		dm.logger,
		deps.VendorFolder,
	)
}

func (dm *DependencyManager) installModules(ctx context.Context, loadResult *deps.LoadResult) error {
	if len(loadResult.Modules) == 0 {
		return nil
	}

	for _, module := range loadResult.Modules {
		if err := dm.installSingleModule(ctx, module); err != nil {
			return fmt.Errorf("install module %s: %w", module.Name.String(), err)
		}
	}

	return nil
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

func (dm *DependencyManager) installSingleModule(ctx context.Context, module deps.LoadedModule) error {
	if dm.isModuleInstalled(module) {
		return nil
	}

	manifest := &deps.Manifest{
		Dependencies: []deps.ManifestDependency{
			{
				Name:    module.Name,
				Version: module.Version,
			},
		},
	}

	loader := &singleModuleLoader{manifest: manifest}
	registryLoader := dm.createModuleLoaderManagerWithLoader(loader)

	loadResult, err := registryLoader.Load(ctx)
	if err != nil {
		return fmt.Errorf("install module %s: %w", module.Name.String(), err)
	}

	if len(loadResult.Modules) == 0 {
		return fmt.Errorf("no modules loaded for %s", module.Name.String())
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

	modulesDir := filepath.Join(filepath.Dir(lockPath), lockFile.Directories.Modules)
	moduleBaseDir := filepath.Join(modulesDir, name.Organization)
	if _, err := os.Stat(moduleBaseDir); err == nil {
		entries, err := os.ReadDir(moduleBaseDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() && strings.HasPrefix(entry.Name(), name.Module+"@") {
					return nil
				}
			}
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

	registryLoader := deps.NewManager(
		organizationClient,
		moduleClient,
		commitClient,
		labelClient,
		downloadClient,
		loader,
		dm.logger,
		lockFile.Directories.Modules,
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

func (dm *DependencyManager) createModuleLoaderManagerWithLoader(loader deps.ManifestLoader) *deps.Manager {
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

	return deps.NewManager(
		organizationClient,
		moduleClient,
		commitClient,
		labelClient,
		downloadClient,
		loader,
		dm.logger,
		deps.VendorFolder,
	)
}

func (dm *DependencyManager) isModuleInstalled(module deps.LoadedModule) bool {
	moduleBaseDir := filepath.Join(deps.VendorFolder, module.Name.Organization)
	if _, err := os.Stat(moduleBaseDir); err != nil {
		return false
	}

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

// CleanupModuleContent removes unused content from module directories
// This function looks inside module directories and removes subdirectories
// that don't match the expected version from the lock file
func (dm *DependencyManager) CleanupModuleContent(_ context.Context, newLockFile *deps.LockFile) []string {
	// Get the modules directory from the new lock file
	modulesDir := newLockFile.Directories.Modules
	if modulesDir == "" {
		modulesDir = ".wippy" // fallback to default
	}

	// Resolve the full path to the modules directory
	modulesPath := filepath.Join(dm.folderPath, modulesDir)

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
func (dm *DependencyManager) CleanupAllUnusedModules(ctx context.Context, _ string, newLockFile *deps.LockFile, stats *deps.ModuleOperationStats) error {
	// First, clean up unused modules (entire module directories)
	removedModules, err := dm.CleanupUnusedModules(ctx, newLockFile)
	if err != nil {
		return fmt.Errorf("cleanup unused modules: %w", err)
	}

	// Add removed modules to stats
	for moduleName, relPath := range removedModules {
		version := dm.ExtractVersionFromPath(relPath)
		if version != "unknown" {
			version = "v" + version
		}
		stats.AddRemoved(moduleName, version)
	}

	return nil
}

// CleanupUnusedModules removes module directories that are not listed in the new lock file
// Returns a list of removed module names for further use
func (dm *DependencyManager) CleanupUnusedModules(_ context.Context, newLockFile *deps.LockFile) (map[string]string, error) {
	// Get the modules directory from the new lock file
	modulesDir := newLockFile.Directories.Modules
	if modulesDir == "" {
		modulesDir = ".wippy" // fallback to default
	}

	// Resolve the full path to the modules directory
	modulesPath := filepath.Join(dm.folderPath, modulesDir)

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
							moduleName := dm.ExtractModuleNameFromPath(moduleSubRelPath)
							if moduleName != "" {
								removedModules[moduleName] = moduleSubRelPath
							}
							break
						}
					}
				} else {
					// This is a simple module directory (old format)
					// Extract module name from the directory path for reporting
					moduleName := dm.ExtractModuleNameFromPath(relPath)
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

// ExtractVersionFromPath extracts the version from a module directory path
// Path format: organization/module@hash/module-version
// Returns the semver version from the module folder name or "unknown" if not found
func (dm *DependencyManager) ExtractVersionFromPath(relPath string) string {
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

type singleModuleLoader struct {
	manifest *deps.Manifest
}

func (l *singleModuleLoader) LoadManifest(_ context.Context) (*deps.Manifest, error) {
	return l.manifest, nil
}
