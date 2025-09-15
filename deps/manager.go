package deps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/Masterminds/semver/v3"
	"github.com/goccy/go-yaml"
	identityv1 "github.com/wippyai/module-registry-proto-go/registry/identity/v1"
	"github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"

	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

// VendorFolder is a name of vendor folder.
const VendorFolder = ".wippy/vendor"

// ManifestLoader provides the way to load manifest information into the manager.
type ManifestLoader interface {
	LoadManifest(ctx context.Context) (*Manifest, error)
}

// Manager manages module loading to the filesystem.
type Manager struct {
	organizationClient identityv1connect.OrganizationServiceClient
	moduleClient       modulev1connect.ModuleServiceClient
	commitClient       modulev1connect.CommitServiceClient
	labelClient        modulev1connect.LabelServiceClient
	downloadClient     modulev1connect.DownloadServiceClient
	loader             ManifestLoader
	vendorFolder       string                      // Custom vendor folder path
	moduleQueue        []ManifestDependency        // Queue for processing modules
	processedModules   map[ManifestDependency]bool // Track processed modules to avoid duplicates
}

func NewManager(
	organizationClient identityv1connect.OrganizationServiceClient,
	moduleClient modulev1connect.ModuleServiceClient,
	commitClient modulev1connect.CommitServiceClient,
	labelClient modulev1connect.LabelServiceClient,
	downloadClient modulev1connect.DownloadServiceClient,
	loader ManifestLoader,
	vendorFolder string,
) *Manager {
	if vendorFolder == "" {
		vendorFolder = VendorFolder
	}

	return &Manager{
		organizationClient: organizationClient,
		moduleClient:       moduleClient,
		commitClient:       commitClient,
		labelClient:        labelClient,
		downloadClient:     downloadClient,
		loader:             loader,
		vendorFolder:       vendorFolder,
		moduleQueue:        make([]ManifestDependency, 0),
		processedModules:   make(map[ManifestDependency]bool),
	}
}

// addRemoteModuleToQueue adds a module to the processing queue if it hasn't been processed yet
func (m *Manager) addRemoteModuleToQueue(dep ManifestDependency) {
	if m.processedModules[dep] {
		// Module already processed, skip
		return
	}

	// Check if module is already in the queue to avoid duplicates
	for _, queuedDep := range m.moduleQueue {
		if queuedDep.Name.Organization == dep.Name.Organization &&
			queuedDep.Name.Module == dep.Name.Module &&
			queuedDep.Version == dep.Version {
			// Module already in queue, skip
			return
		}
	}

	m.moduleQueue = append(m.moduleQueue, dep)
}

// Load resolves and downloads dependencies based on the manifest
func (m *Manager) Load(ctx context.Context) (*LoadResult, error) {
	// Reset processed modules map for clean state
	m.processedModules = make(map[ManifestDependency]bool)
	m.moduleQueue = make([]ManifestDependency, 0)

	manifest, err := m.loader.LoadManifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}

	// Separate local and remote dependencies
	localModules := make(map[Name]string)
	remoteDeps := make([]ManifestDependency, 0, len(manifest.Dependencies))

	for _, dep := range manifest.Dependencies {
		if dep.Path != "" {
			localModules[dep.Name] = dep.Path
			continue
		}
		remoteDeps = append(remoteDeps, dep)
	}

	// Process remote dependencies
	remoteModules, err := m.processRemoteDependencies(ctx, remoteDeps)
	if err != nil {
		return nil, fmt.Errorf("process remote dependencies: %w", err)
	}

	// Process local modules
	localLoadedModules, err := m.processLocalModules(localModules)
	if err != nil {
		return nil, fmt.Errorf("process local modules: %w", err)
	}

	// Combine all loaded modules
	allModules := make([]LoadedModule, 0, len(remoteModules)+len(localLoadedModules))
	allModules = append(allModules, remoteModules...)
	allModules = append(allModules, localLoadedModules...)

	return &LoadResult{
		Modules: allModules,
	}, nil
}

type dependencyInformation struct {
	organization       *identityv1.Organization
	module             *modulev1.Module
	labels             []*modulev1.Label
	matchingLabelIndex int
}

// processRemoteDependencies handles fetching and storing remote dependencies using a queue-based approach
func (m *Manager) processRemoteDependencies(ctx context.Context, manifestDependencies []ManifestDependency) ([]LoadedModule, error) {
	if len(manifestDependencies) == 0 {
		return nil, nil
	}

	// Add initial dependencies to queue
	for _, dep := range manifestDependencies {
		m.addRemoteModuleToQueue(dep)
	}

	var loadedModules []LoadedModule

	// Process queue until empty
	for len(m.moduleQueue) > 0 {
		// Get next module from queue
		dep := m.moduleQueue[0]
		m.moduleQueue = m.moduleQueue[1:]

		// Skip if already processed
		if m.processedModules[dep] {
			continue
		}

		// Mark as processed
		m.processedModules[dep] = true

		// Process the module
		loadedModule, err := m.downloadAndStoreModule(ctx, dep)
		if err != nil {
			return nil, fmt.Errorf("process module %s: %w", dep, err)
		}

		if loadedModule != nil {
			loadedModules = append(loadedModules, *loadedModule)
		}
	}

	return loadedModules, nil
}

// downloadAndStoreModule downloads and stores a single module
func (m *Manager) downloadAndStoreModule(ctx context.Context, dep ManifestDependency) (*LoadedModule, error) {
	// Get module information
	info, err := m.getModuleInfo(ctx, dep)
	if err != nil {
		return nil, err
	}

	// Download module
	moduleDir, err := m.downloadModule(ctx, dep, info)
	if err != nil {
		return nil, err
	}

	// Find the subdirectory where files are actually stored
	subdirPath, err := m.findModuleSubdir(moduleDir)
	if err != nil {
		return nil, err
	}

	// Check for new dependencies in the subdirectory where files are stored
	if err := m.findNewDependencies(ctx, subdirPath); err != nil {
		return nil, err
	}

	return &LoadedModule{
		Name:         dep.Name,
		Version:      info.labels[info.matchingLabelIndex].GetName(),
		Path:         moduleDir, // Use the parent directory - filesystem walking will find subdirectory files
		Organization: dep.Name.Organization,
		Module:       dep.Name.Module,
	}, nil
}

// findModuleSubdir finds the subdirectory where module files are stored
func (m *Manager) findModuleSubdir(moduleDir string) (string, error) {
	entries, err := os.ReadDir(moduleDir)
	if err != nil {
		return "", fmt.Errorf("read module directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "module-") { // todo: remove this valuable piece of code
			return filepath.Join(moduleDir, entry.Name()), nil
		}
	}

	return "", fmt.Errorf("no module subdirectory found in %s", moduleDir)
}

// getModuleInfo fetches all information needed for a module
func (m *Manager) getModuleInfo(ctx context.Context, dep ManifestDependency) (*dependencyInformation, error) {
	deps := map[ManifestDependency]*dependencyInformation{dep: {}}

	err := errors.Join(
		m.fetchOrganizationInformation(ctx, deps),
		m.fetchModuleInformation(ctx, deps),
		m.fetchLabelInformation(ctx, deps),
	)
	if err != nil {
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

// downloadModule downloads and stores the module files
func (m *Manager) downloadModule(ctx context.Context, dep ManifestDependency, info *dependencyInformation) (string, error) {
	if err := os.MkdirAll(m.vendorFolder, os.ModePerm); err != nil {
		return "", fmt.Errorf("create vendor folder: %w", err)
	}

	if len(info.labels) == 0 {
		return "", fmt.Errorf("no labels found for %s", dep)
	}

	commitID := info.labels[info.matchingLabelIndex].GetCommitId()

	resp, err := m.downloadClient.Download(ctx, connect.NewRequest(&modulev1.DownloadRequest{CommitIds: []string{commitID}}))
	if err != nil {
		return "", fmt.Errorf("download commit %s: %w", commitID, err)
	}

	downloadedContents := resp.Msg.GetContents()
	if len(downloadedContents) == 0 {
		return "", fmt.Errorf("no content downloaded for commit %s", commitID)
	}

	content := downloadedContents[0]
	moduleDir := filepath.Join(m.vendorFolder, dep.Name.Organization, dep.Name.Module+"@"+content.GetCommit().GetId())

	if err := m.storeModuleFiles(moduleDir, content.GetFiles()); err != nil {
		return "", fmt.Errorf("store module files: %w", err)
	}

	return moduleDir, nil
}

// findNewDependencies scans the module for new dependencies and adds them to the queue
func (m *Manager) findNewDependencies(_ context.Context, moduleDir string) error {
	return filepath.Walk(moduleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		// Only process YAML files
		name := strings.ToLower(info.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}

		// Read and parse YAML
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read YAML file %s: %w", path, err)
		}

		// Try standard Manifest format
		var manifest Manifest
		if err := yaml.Unmarshal(data, &manifest); err == nil && len(manifest.Dependencies) > 0 {
			for _, dep := range manifest.Dependencies {
				if dep.Path == "" { // Only remote dependencies
					m.addRemoteModuleToQueue(dep)
				}
			}
			return nil
		}

		// Try entries format
		var entriesDoc struct {
			Entries []struct {
				Name      string `yaml:"name"`
				Kind      string `yaml:"kind"`
				Component string `yaml:"component"`
				Version   string `yaml:"version"`
			} `yaml:"entries"`
		}

		if err := yaml.Unmarshal(data, &entriesDoc); err == nil {
			for _, entry := range entriesDoc.Entries {
				if entry.Kind == "ns.dependency" && entry.Component != "" {
					parts := strings.Split(entry.Component, "/")
					if len(parts) == 2 {
						dep := ManifestDependency{
							Name:    Name{Organization: parts[0], Module: parts[1]},
							Version: entry.Version,
						}
						m.addRemoteModuleToQueue(dep)
					}
				}
			}
		}

		return nil
	})
}

func (m *Manager) fetchOrganizationInformation(ctx context.Context, deps map[ManifestDependency]*dependencyInformation) error {
	organizationNames := make([]*identityv1.OrganizationRef, 0, len(deps))
	seen := make(map[string]struct{})
	for md := range deps {
		if _, ok := seen[md.Name.Organization]; ok {
			continue
		}
		organizationNames = append(organizationNames, &identityv1.OrganizationRef{Value: &identityv1.OrganizationRef_Name{Name: md.Name.Organization}})
		seen[md.Name.Organization] = struct{}{}
	}

	// Find organization for the dependencies
	resp, err := m.organizationClient.ListOrganizations(ctx, connect.NewRequest(&identityv1.ListOrganizationsRequest{Refs: organizationNames}))
	if err != nil {
		return fmt.Errorf("list organizations: %w", err)
	}
	listedOrganizations := resp.Msg.GetOrganizations()

	for md, info := range deps {
		organizationName := md.Name.Organization
		matchesName := func(o *identityv1.Organization) bool { return organizationName == o.GetName() }
		i := slices.IndexFunc(listedOrganizations, matchesName)
		if i == -1 {
			return fmt.Errorf("organization %s not found", organizationName)
		}
		info.organization = listedOrganizations[i]
	}
	return nil
}

func (m *Manager) fetchModuleInformation(ctx context.Context, deps map[ManifestDependency]*dependencyInformation) error {
	// Find modules for the dependencies
	moduleRefs := make([]*modulev1.ModuleRef, 0, len(deps))
	for md, info := range deps {
		if info.organization == nil {
			return fmt.Errorf("missing organization info: %v", md)
		}
		moduleRefs = append(moduleRefs, &modulev1.ModuleRef{
			Value: &modulev1.ModuleRef_NameRef{NameRef: &modulev1.ModuleRef_ModuleNameRef{
				OrganizationId: info.organization.GetId(),
				Name:           md.Name.Module,
			}},
		})
	}

	resp, err := m.moduleClient.ListModules(ctx, connect.NewRequest(&modulev1.ListModulesRequest{Refs: moduleRefs}))
	if err != nil {
		return fmt.Errorf("list modules: %w", err)
	}
	listedModules := resp.Msg.GetModules()
	for md, info := range deps {
		moduleName := md.Name.Module
		matchesName := func(m *modulev1.Module) bool { return moduleName == m.GetName() }
		i := slices.IndexFunc(listedModules, matchesName)
		if i == -1 {
			return fmt.Errorf("module %s not found", moduleName)
		}
		info.module = listedModules[i]
	}
	return nil
}

func (m *Manager) fetchLabelInformation(ctx context.Context, deps map[ManifestDependency]*dependencyInformation) error {
	moduleIDs := make([]string, 0, len(deps))
	for md, info := range deps {
		if info.module == nil {
			return fmt.Errorf("missing module info: %v", md)
		}
		moduleIDs = append(moduleIDs, info.module.GetId())
	}

	resp, err := m.labelClient.ListModuleLabels(ctx, connect.NewRequest(&modulev1.ListModuleLabelsRequest{ModuleIds: moduleIDs}))
	if err != nil {
		return fmt.Errorf("list module labels: %w", err)
	}
	listedLabels := resp.Msg.GetLabels()
	moduleIDLabels := make(map[string][]*modulev1.Label, len(listedLabels))
	for _, label := range listedLabels {
		moduleIDLabels[label.GetModuleId()] = append(moduleIDLabels[label.GetModuleId()], label)
	}
	for md, info := range deps {
		moduleID := info.module.GetId()
		labels, ok := moduleIDLabels[moduleID]
		if !ok {
			return fmt.Errorf("missing module labels: %v", md)
		}
		info.labels = labels
	}
	return nil
}

// storeModuleFiles stores module files in the given directory
func (m *Manager) storeModuleFiles(moduleDir string, files []*modulev1.File) error {
	if len(files) == 0 {
		return nil
	}

	// Get subdirectory name from first file
	firstPath := files[0].GetPath()
	pathParts := strings.Split(firstPath, "/")
	if len(pathParts) == 0 {
		return fmt.Errorf("invalid file path: %s", firstPath)
	}

	moduleSubdir := pathParts[0]
	subdirPath := filepath.Join(moduleDir, moduleSubdir)

	// Create subdirectory
	if err := os.MkdirAll(subdirPath, os.ModePerm); err != nil {
		return fmt.Errorf("create module subdirectory: %w", err)
	}

	// Write files to subdirectory
	for _, file := range files {
		filePath := file.GetPath()

		// Remove module subdirectory prefix if present
		if strings.HasPrefix(filePath, moduleSubdir+"/") {
			filePath = strings.TrimPrefix(filePath, moduleSubdir+"/")
		}

		fullPath := filepath.Join(subdirPath, filePath)

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}

		// Write file
		if err := os.WriteFile(fullPath, file.GetContent(), 0600); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
	}

	return nil
}

// processLocalModules processes local modules
func (m *Manager) processLocalModules(localModules map[Name]string) ([]LoadedModule, error) {
	loadedModules := make([]LoadedModule, 0, len(localModules))

	for name, path := range localModules {
		localOS, err := os.OpenRoot(path)
		if err != nil {
			return nil, fmt.Errorf("open local module: %w", err)
		}

		modulePath := filepath.Join(m.vendorFolder, name.Organization, name.Module+"@local")
		if err := os.CopyFS(modulePath, localOS.FS()); err != nil {
			return nil, fmt.Errorf("copy %s: %w", name, err)
		}

		// Create LoadedModule entry for local module
		loadedModule := LoadedModule{
			Name:         name,
			Version:      "local",
			Path:         modulePath,
			Organization: name.Organization,
			Module:       name.Module,
		}
		loadedModules = append(loadedModules, loadedModule)
	}

	return loadedModules, nil
}

func findHighestMatchingVersion(version string, availableLabels []*modulev1.Label) (int, error) {
	if len(availableLabels) == 0 {
		return -1, errors.New("no labels available")
	}

	// Parse constraint string
	constraint, err := parseConstraint(version)
	if err != nil {
		return -1, fmt.Errorf("parse constraint: %w", err)
	}

	type versionLabel struct {
		version    *semver.Version
		labelIndex int
	}
	// Filter and sort matching versions
	var matchingVersions []versionLabel
	for i, label := range availableLabels {
		labelName := label.GetName()
		// Try to parse as semver
		labelVersion, err := semver.NewVersion(strings.TrimPrefix(labelName, "v"))
		if err != nil {
			return -1, fmt.Errorf("parse label version: %w", err)
		}

		// Check if version matches the constraint
		if constraint.Check(labelVersion) {
			matchingVersions = append(matchingVersions, versionLabel{version: labelVersion, labelIndex: i})
		}
	}

	if len(matchingVersions) == 0 {
		return -1, errors.New("no version matches")
	}

	slices.SortFunc(matchingVersions, func(a, b versionLabel) int {
		return a.version.Compare(b.version)
	})
	slices.Reverse(matchingVersions)

	return matchingVersions[0].labelIndex, nil
}

// parseConstraint converts a constraint string to a semver constraint
func parseConstraint(constraintStr string) (*semver.Constraints, error) {
	// Handle empty constraint - match any version
	if constraintStr == "" {
		return semver.NewConstraint("*")
	}

	// Check if this appears to be a standard constraint string
	if strings.Contains(constraintStr, ">") ||
		strings.Contains(constraintStr, "<") ||
		strings.Contains(constraintStr, "=") {
		// Clean up the constraint string by removing v prefixes
		cleanConstraint := strings.ReplaceAll(constraintStr, "v", "")
		return semver.NewConstraint(cleanConstraint)
	}

	// Assume it's an exact version match if it doesn't contain operators
	// This allows formats like "identityv1.2.3" to be treated as "= 1.2.3"
	version := strings.TrimPrefix(constraintStr, "v")

	// If it's an exact version, create an equals constraint
	return semver.NewConstraint("=" + version)
}
