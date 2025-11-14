package deps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"connectrpc.com/connect"
	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
	"go.uber.org/zap"
)

// DependencyProcessor handles dependency processing logic
type DependencyProcessor struct {
	apiService       *APIService
	yamlParser       *YAMLParser
	fileService      *FileService
	downloadClient   modulev1connect.DownloadServiceClient
	logger           *zap.Logger
	vendorFolder     string
	moduleQueue      []ManifestDependency
	processedModules map[ManifestDependency]bool
	moduleStats      []ModuleStats
}

// NewDependencyProcessor creates a new dependency processor
func NewDependencyProcessor(
	apiService *APIService,
	yamlParser *YAMLParser,
	fileService *FileService,
	downloadClient modulev1connect.DownloadServiceClient,
	logger *zap.Logger,
	vendorFolder string,
) *DependencyProcessor {
	return &DependencyProcessor{
		apiService:       apiService,
		yamlParser:       yamlParser,
		fileService:      fileService,
		downloadClient:   downloadClient,
		logger:           logger,
		vendorFolder:     vendorFolder,
		moduleQueue:      make([]ManifestDependency, 0),
		processedModules: make(map[ManifestDependency]bool),
		moduleStats:      make([]ModuleStats, 0),
	}
}

// ProcessRemoteDependencies processes remote dependencies using a queue-based approach
func (dp *DependencyProcessor) ProcessRemoteDependencies(ctx context.Context, manifestDependencies []ManifestDependency) ([]LoadedModule, error) {
	if len(manifestDependencies) == 0 {
		return nil, nil
	}

	// Reset state
	dp.processedModules = make(map[ManifestDependency]bool)
	dp.moduleQueue = make([]ManifestDependency, 0)
	dp.moduleStats = make([]ModuleStats, 0)

	// Add initial dependencies to queue
	for _, dep := range manifestDependencies {
		dp.addModuleToQueue(dep)
	}

	var loadedModules []LoadedModule

	// Process queue until empty
	for len(dp.moduleQueue) > 0 {
		// Get next module from queue
		dep := dp.moduleQueue[0]
		dp.moduleQueue = dp.moduleQueue[1:]

		// Skip if already processed
		if dp.processedModules[dep] {
			continue
		}

		// Mark as processed
		dp.processedModules[dep] = true

		// Process the module
		loadedModule, err := dp.processModule(ctx, dep)
		if err != nil {
			return nil, fmt.Errorf("process module %s: %w", dep, err)
		}

		if loadedModule != nil {
			loadedModules = append(loadedModules, *loadedModule)
		}
	}

	return loadedModules, nil
}

// processModule processes a single module
func (dp *DependencyProcessor) processModule(ctx context.Context, dep ManifestDependency) (*LoadedModule, error) {
	// Get module information
	info, err := dp.getModuleInfo(ctx, dep)
	if err != nil {
		return nil, fmt.Errorf("get module info: %w", err)
	}

	// Get the commit ID and version for logging
	commitID := info.labels[info.matchingLabelIndex].GetCommitId()
	version := info.labels[info.matchingLabelIndex].GetName()
	moduleName := fmt.Sprintf("%s/%s", dep.Name.Organization, dep.Name.Module)

	// Check if module is already in cache
	if dp.isModuleInCache(moduleName, commitID) {
		return dp.createCachedModule(dep, commitID, version, moduleName)
	}

	// Download and store module
	moduleDir, err := dp.downloadAndStoreModule(ctx, dep, info)
	if err != nil {
		return nil, fmt.Errorf("download and store module: %w", err)
	}

	// Find the subdirectory where files are actually stored
	subdirPath, err := dp.fileService.FindModuleSubdir(moduleDir)
	if err != nil {
		return nil, fmt.Errorf("find module subdir: %w", err)
	}

	// Check for new dependencies in the subdirectory
	if err := dp.findNewDependencies(ctx, subdirPath); err != nil {
		return nil, fmt.Errorf("find new dependencies: %w", err)
	}

	// Record statistics for download
	dp.moduleStats = append(dp.moduleStats, ModuleStats{
		Name:    moduleName,
		Version: version,
		Status:  StatusDownloaded,
	})

	return &LoadedModule{
		Name:         dep.Name,
		Version:      version,
		Path:         moduleDir,
		Organization: dep.Name.Organization,
		Module:       dep.Name.Module,
	}, nil
}

// addModuleToQueue adds a module to the processing queue if it hasn't been processed yet
func (dp *DependencyProcessor) addModuleToQueue(dep ManifestDependency) {
	if dp.processedModules[dep] {
		return
	}

	// Check if module is already in the queue to avoid duplicates
	for _, queuedDep := range dp.moduleQueue {
		if queuedDep.Name.Organization == dep.Name.Organization &&
			queuedDep.Name.Module == dep.Name.Module &&
			queuedDep.Version == dep.Version {
			return
		}
	}

	dp.moduleQueue = append(dp.moduleQueue, dep)
}

// isModuleInCache checks if a module already exists in the cache
func (dp *DependencyProcessor) isModuleInCache(moduleName, hash string) bool {
	name, err := ParseName(moduleName)
	if err != nil {
		dp.logger.Debug("Failed to parse module name for cache check",
			zap.String("module", moduleName),
			zap.Error(err))
		return false
	}

	moduleDir := filepath.Join(dp.vendorFolder, name.Organization, name.Module)
	if _, err := os.Stat(moduleDir); err == nil {
		dp.logger.Debug("Module found in cache",
			zap.String("module", moduleName),
			zap.String("hash", hash),
			zap.String("path", moduleDir))
		return true
	}

	dp.logger.Debug("Module not found in cache",
		zap.String("module", moduleName),
		zap.String("hash", hash),
		zap.String("expected_path", moduleDir))
	return false
}

// createCachedModule creates a LoadedModule for a cached module
func (dp *DependencyProcessor) createCachedModule(dep ManifestDependency, commitID, version, moduleName string) (*LoadedModule, error) {
	// Record statistics for cache hit
	dp.moduleStats = append(dp.moduleStats, ModuleStats{
		Name:    moduleName,
		Version: version,
		Status:  StatusFromCache,
	})

	// Return the existing module path
	moduleDir := filepath.Join(dp.vendorFolder, dep.Name.Organization, dep.Name.Module)
	return &LoadedModule{
		Name:         dep.Name,
		Version:      version,
		Path:         moduleDir,
		Organization: dep.Name.Organization,
		Module:       dep.Name.Module,
	}, nil
}

// getModuleInfo fetches all information needed for a module
func (dp *DependencyProcessor) getModuleInfo(ctx context.Context, dep ManifestDependency) (*dependencyInformation, error) {
	deps := map[ManifestDependency]*dependencyInformation{dep: {}}

	if err := dp.apiService.FetchDependencyInformation(ctx, deps); err != nil {
		return nil, fmt.Errorf("fetch module information: %w", err)
	}

	info := deps[dep]
	li, err := findHighestMatchingVersion(dep.Version, info.labels)
	if err != nil {
		return nil, fmt.Errorf("find highest matching version: %w", err)
	}
	info.matchingLabelIndex = li

	return info, nil
}

// downloadAndStoreModule downloads and stores module files
func (dp *DependencyProcessor) downloadAndStoreModule(ctx context.Context, dep ManifestDependency, info *dependencyInformation) (string, error) {
	if err := dp.fileService.EnsureDirectoryExists(dp.vendorFolder); err != nil {
		return "", fmt.Errorf("create vendor folder: %w", err)
	}

	if len(info.labels) == 0 {
		return "", fmt.Errorf("no labels found for %s", dep)
	}

	commitID := info.labels[info.matchingLabelIndex].GetCommitId()

	resp, err := dp.downloadClient.Download(ctx, connect.NewRequest(&modulev1.DownloadRequest{CommitIds: []string{commitID}}))
	if err != nil {
		return "", fmt.Errorf("download commit %s: %w", commitID, err)
	}

	downloadedContents := resp.Msg.GetContents()
	if len(downloadedContents) == 0 {
		return "", fmt.Errorf("no content downloaded for commit %s", commitID)
	}

	content := downloadedContents[0]
	moduleDir := filepath.Join(dp.vendorFolder, dep.Name.Organization, dep.Name.Module)

	if err := dp.fileService.StoreModuleFiles(moduleDir, content.GetFiles()); err != nil {
		return "", fmt.Errorf("store module files: %w", err)
	}

	return moduleDir, nil
}

// findNewDependencies scans the module for new dependencies and adds them to the queue
func (dp *DependencyProcessor) findNewDependencies(_ context.Context, moduleDir string) error {
	return dp.yamlParser.ParseDependenciesFromDirectory(moduleDir, func(dep ManifestDependency) {
		dp.addModuleToQueue(dep)
	})
}

// GetModuleStats returns the collected module statistics
func (dp *DependencyProcessor) GetModuleStats() []ModuleStats {
	return dp.moduleStats
}
