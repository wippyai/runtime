package moduleloader

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
	identityv1 "github.com/wippyai/module-registry-proto-go/registry/identity/v1"
	"github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"

	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

// VendorFolder is a name of vendor folder.
const VendorFolder = ".wippy"

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
	vendorFolder       string // Custom vendor folder path
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
	}
}

// Load resolves and downloads dependencies based on the manifest
func (m *Manager) Load(ctx context.Context) error {
	manifest, err := m.loader.LoadManifest(ctx)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
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
	if err := m.processRemoteDependencies(ctx, remoteDeps); err != nil {
		return fmt.Errorf("process remote dependencies: %w", err)
	}

	// Process local modules
	if err := m.processLocalModules(localModules); err != nil {
		return fmt.Errorf("process local modules: %w", err)
	}

	return nil
}

type dependencyInformation struct {
	organization       *identityv1.Organization
	module             *modulev1.Module
	labels             []*modulev1.Label
	matchingLabelIndex int
}

// processRemoteDependencies handles fetching and storing remote dependencies
func (m *Manager) processRemoteDependencies(ctx context.Context, manifestDependencies []ManifestDependency) error {
	if len(manifestDependencies) == 0 {
		return nil
	}

	deps, err := m.fetchRemoteDependencyInformation(ctx, manifestDependencies)
	if err != nil {
		return fmt.Errorf("fetch remote dependencies: %w", err)
	}

	for md, info := range deps {
		li, err := findHighestMatchingVersion(md.Version, info.labels)
		if err != nil {
			return fmt.Errorf("find %s highest matching version: %w", md, err)
		}
		info.matchingLabelIndex = li
	}

	if err := m.downloadAndStoreModules(ctx, deps); err != nil {
		return fmt.Errorf("download and store modules: %w", err)
	}

	return nil
}

// downloadAndStoreModules downloads and stores modules by their commit IDs
func (m *Manager) downloadAndStoreModules(ctx context.Context, deps map[ManifestDependency]*dependencyInformation) error {
	// Ensure .wippy directory exists
	if err := os.MkdirAll(m.vendorFolder, os.ModePerm); err != nil {
		return fmt.Errorf("create vendor folder: %w", err)
	}

	requiredCommits := make([]string, 0, len(deps))
	for md, info := range deps {
		if len(info.labels) == 0 {
			return fmt.Errorf("no labels found for %s", md)
		}
		requiredCommits = append(requiredCommits, info.labels[info.matchingLabelIndex].GetCommitId())
	}
	// todo: add pagination or streaming to avoid downloading all at once
	resp, err := m.downloadClient.Download(ctx, connect.NewRequest(&modulev1.DownloadRequest{CommitIds: requiredCommits}))
	if err != nil {
		return fmt.Errorf("download required commits: %w", err)
	}
	downloadedContents := resp.Msg.GetContents()

	downloadedDependencies := make(map[ManifestDependency]*modulev1.DownloadResponse_Content, len(deps))
	// Make sure we have all data in place before write
	for md, info := range deps {
		labelCommit := info.labels[info.matchingLabelIndex].GetCommitId()
		matchesCommit := func(content *modulev1.DownloadResponse_Content) bool {
			return content.GetCommit().GetId() == labelCommit
		}
		i := slices.IndexFunc(downloadedContents, matchesCommit)
		if i == -1 {
			return fmt.Errorf("downloaded content for %s missing label commit %s", md, labelCommit)
		}
		downloadedDependencies[md] = downloadedContents[i]
	}

	for md, content := range downloadedDependencies {
		orgName := md.Name.Organization
		moduleName := md.Name.Module

		moduleDir := filepath.Join(m.vendorFolder, orgName, moduleName+"@"+content.GetCommit().GetId())
		// Store module files
		err := m.storeModuleFiles(moduleDir, content.GetFiles())
		if err != nil {
			return fmt.Errorf("store module files for %s: %w", md, err)
		}
	}
	return nil
}

func (m *Manager) fetchRemoteDependencyInformation(ctx context.Context, manifestDependencies []ManifestDependency) (
	map[ManifestDependency]*dependencyInformation,
	error,
) {
	deps := make(map[ManifestDependency]*dependencyInformation)
	for _, md := range manifestDependencies {
		deps[md] = &dependencyInformation{}
	}

	err := errors.Join(
		m.fetchOrganizationInformation(ctx, deps),
		m.fetchModuleInformation(ctx, deps),
		m.fetchLabelInformation(ctx, deps),
	)
	if err != nil {
		return nil, fmt.Errorf("fetch module dependencies information: %w", err)
	}

	return deps, nil
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
	// Create module directory
	if err := os.MkdirAll(moduleDir, os.ModePerm); err != nil {
		return fmt.Errorf("create module directory: %w", err)
	}

	// Write files
	for _, file := range files {
		filePath := filepath.Join(moduleDir, file.GetPath())

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}

		// Write file content
		if err := os.WriteFile(filePath, file.GetContent(), 0600); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
	}

	return nil
}

// processLocalModules processes local modules
func (m *Manager) processLocalModules(localModules map[Name]string) error {
	for name, path := range localModules {
		localOS, err := os.OpenRoot(path)
		if err != nil {
			return fmt.Errorf("open local module: %w", err)
		}
		if err := os.CopyFS(filepath.Join(m.vendorFolder, name.Organization, name.Module+"@local"), localOS.FS()); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}

	return nil
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
