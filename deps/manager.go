package deps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	identityv1 "github.com/wippyai/module-registry-proto-go/registry/identity/v1"
	"github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
	"go.uber.org/zap"

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
	downloadClient      modulev1connect.DownloadServiceClient
	loader              ManifestLoader
	logger              *zap.Logger
	vendorFolder        string
	dependencyProcessor *DependencyProcessor
}

func NewManager(
	organizationClient identityv1connect.OrganizationServiceClient,
	moduleClient modulev1connect.ModuleServiceClient,
	_ modulev1connect.CommitServiceClient,
	labelClient modulev1connect.LabelServiceClient,
	downloadClient modulev1connect.DownloadServiceClient,
	loader ManifestLoader,
	logger *zap.Logger,
	vendorFolder string,
) *Manager {
	if vendorFolder == "" {
		vendorFolder = VendorFolder
	}

	// Create services
	apiService := NewAPIService(organizationClient, moduleClient, labelClient)
	yamlParser := NewYAMLParser(logger)
	fileService := NewFileService(logger)
	dependencyProcessor := NewDependencyProcessor(apiService, yamlParser, fileService, downloadClient, logger, vendorFolder)

	return &Manager{
		downloadClient:      downloadClient,
		loader:              loader,
		logger:              logger,
		vendorFolder:        vendorFolder,
		dependencyProcessor: dependencyProcessor,
	}
}

// Load resolves and downloads dependencies based on the manifest
func (m *Manager) Load(ctx context.Context) (*LoadResult, error) {
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
	remoteModules, err := m.dependencyProcessor.ProcessRemoteDependencies(ctx, remoteDeps)
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
		Modules:     allModules,
		ModuleStats: m.dependencyProcessor.GetModuleStats(),
	}, nil
}

type dependencyInformation struct {
	organization       *identityv1.Organization
	module             *modulev1.Module
	labels             []*modulev1.Label
	matchingLabelIndex int
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

		// Record statistics for local module
		moduleName := fmt.Sprintf("%s/%s", name.Organization, name.Module)
		m.dependencyProcessor.moduleStats = append(m.dependencyProcessor.moduleStats, ModuleStats{
			Name:    moduleName,
			Version: "local",
			Status:  StatusFromReplacement,
		})

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
