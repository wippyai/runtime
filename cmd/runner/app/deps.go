package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

type singleModuleLoader struct {
	manifest *deps.Manifest
}

func (l *singleModuleLoader) LoadManifest(_ context.Context) (*deps.Manifest, error) {
	return l.manifest, nil
}
