package moduleloader

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"

	modulev1 "github.com/wippyai/module-registry-proto/gen/registry/module/v1"
	"github.com/wippyai/module-registry-proto/gen/registry/module/v1/modulev1connect"
)

// VendorFolder is a name of vendor folder.
const VendorFolder = ".wippy"

// ManifestLoader provides the way to load manifest information into the manager.
type ManifestLoader interface {
	LoadManifest(ctx context.Context) (*Manifest, error)
}

// Manager manages module loading to the filesystem.
type Manager struct {
	downloadClient modulev1connect.DownloadServiceClient
	uploadClient   modulev1connect.UploadServiceClient
	moduleClient   modulev1connect.ModuleServiceClient
	graphClient    modulev1connect.GraphServiceClient

	loader ManifestLoader
}

// NewManager returns new Manager instance.
func NewManager(baseURL string) *Manager {
	client := &http.Client{Timeout: time.Second * 10}
	return &Manager{
		downloadClient: modulev1connect.NewDownloadServiceClient(client, baseURL, connect.WithProtoJSON()),
		uploadClient:   modulev1connect.NewUploadServiceClient(client, baseURL, connect.WithProtoJSON()),
		moduleClient:   modulev1connect.NewModuleServiceClient(client, baseURL, connect.WithProtoJSON()),
		graphClient:    modulev1connect.NewGraphServiceClient(client, baseURL, connect.WithProtoJSON()),
		loader:         FilesystemLoader{},
	}
}

func (m *Manager) Load(ctx context.Context) error {
	loadedManifest, err := m.loader.LoadManifest(ctx)
	if err != nil {
		return fmt.Errorf("load loadedManifest: %w", err)
	}

	// Collect refs
	localModules := make(map[string]string)
	refs := make([]ManifestDependency, 0, len(loadedManifest.Dependencies))
	for _, dependency := range loadedManifest.Dependencies {
		if dependency.Path != "" {
			localModules[dependency.Name] = dependency.Path
			continue
		}
		refs = append(refs, dependency)
	}

	commits, err := m.getAllGraphCommits(ctx, refs)
	if err != nil {
		return fmt.Errorf("get all graph commits: %w", err)
	}

	contents, err := m.downloadCommits(ctx, commits)
	if err != nil {
		return fmt.Errorf("download commits: %w", err)
	}

	moduleIds := make([]string, 0, len(contents))
	for _, item := range contents {
		moduleIds = append(moduleIds, item.GetCommit().GetModuleId())
	}

	modules, err := m.listModules(ctx, moduleIds)
	if err != nil {
		return fmt.Errorf("list modules: %w", err)
	}

	if err := m.writeContents(contents, modules, localModules); err != nil {
		return fmt.Errorf("write files: %w", err)
	}

	return nil
}

func (m *Manager) downloadCommits(ctx context.Context, commits []*modulev1.Commit) ([]*modulev1.DownloadResponse_Content, error) {
	refs := make([]*modulev1.ResourceRef, 0, len(commits))
	for _, commit := range commits {
		refs = append(refs, &modulev1.ResourceRef{
			Value: &modulev1.ResourceRef_Id{Id: commit.Id},
		})
	}

	resp, err := m.downloadClient.Download(ctx, &connect.Request[modulev1.DownloadRequest]{Msg: &modulev1.DownloadRequest{
		ResourceRefs: refs,
	}})
	if err != nil {
		return nil, fmt.Errorf("download commits: %w", err)
	}

	return resp.Msg.GetContents(), nil
}

func (m *Manager) getAllGraphCommits(ctx context.Context, deps []ManifestDependency) ([]*modulev1.Commit, error) {
	refs := make([]*modulev1.ResourceRef, 0, len(deps))
	for _, dep := range deps {
		if dep.Path != "" {
			// if path not empty it means dependency local
			continue
		}
		split := strings.Split(dep.Name, "/")
		if len(split) != 2 {
			return nil, fmt.Errorf("invalid dependency: %s", dep.Name)
		}

		if isLabel(dep.Version) {
			refs = append(refs, &modulev1.ResourceRef{
				Value: &modulev1.ResourceRef_Name_{
					Name: &modulev1.ResourceRef_Name{
						Organization: split[0],
						Module:       split[1],
						Version:      &modulev1.ResourceRef_Name_Label{Label: dep.Version},
					},
				},
			})
			continue
		}

		refs = append(refs, &modulev1.ResourceRef{
			Value: &modulev1.ResourceRef_Name_{
				Name: &modulev1.ResourceRef_Name{
					Organization: split[0],
					Module:       split[1],
					Version:      &modulev1.ResourceRef_Name_Ref{Ref: dep.Version},
				},
			},
		})
	}
	resp, err := m.graphClient.GetGraph(ctx, &connect.Request[modulev1.GetGraphRequest]{
		Msg: &modulev1.GetGraphRequest{ResourceRefs: refs},
	})
	if err != nil {
		return nil, fmt.Errorf("get graph: %w", err)
	}
	return resp.Msg.GetGraph().GetCommits(), nil
}

func (m *Manager) listModules(ctx context.Context, ids []string) (map[string]*modulev1.Module, error) {
	refs := make([]*modulev1.ModuleRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, &modulev1.ModuleRef{Value: &modulev1.ModuleRef_Id{Id: id}})
	}

	resp, err := m.moduleClient.ListModules(ctx, &connect.Request[modulev1.ListModulesRequest]{
		Msg: &modulev1.ListModulesRequest{
			ModuleRefs: refs,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list modules: %w", err)
	}

	modules := make(map[string]*modulev1.Module)
	for _, module := range resp.Msg.GetModules() {
		modules[module.GetId()] = module
	}

	return modules, nil
}

func (*Manager) writeContents(
	contents []*modulev1.DownloadResponse_Content,
	modules map[string]*modulev1.Module,
	localModules map[string]string,
) error {
	if err := os.RemoveAll(VendorFolder); err != nil {
		return fmt.Errorf("remove vendor folder: %w", err)
	}

	for _, content := range contents {
		module, ok := modules[content.GetCommit().GetModuleId()]
		if !ok {
			return fmt.Errorf("module %s not found", content.GetCommit().GetModuleId())
		}

		for i := range content.GetFiles() {
			path := filepath.Join(
				VendorFolder,
				module.GetOrganizationName(),
				module.GetName()+"@"+content.GetCommit().GetDigest(),
				content.GetFiles()[i].GetPath(),
			)

			if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
				return fmt.Errorf("create directory: %w", err)
			}

			if err := os.WriteFile(path, content.GetFiles()[i].GetContent(), 0600); err != nil {
				return fmt.Errorf("write file: %w", err)
			}
		}
	}

	for name, path := range localModules {
		localOS, err := os.OpenRoot(path)
		if err != nil {
			return fmt.Errorf("open local module: %w", err)
		}
		if err := os.CopyFS(filepath.Join(VendorFolder, name+"@local"), localOS.FS()); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}
	return nil
}

// todo: better label detection
func isLabel(s string) bool {
	return len(s) <= 60
}
