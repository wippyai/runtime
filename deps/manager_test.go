package deps_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponyruntime/pony/deps"
	mock_identityv1connect "github.com/ponyruntime/pony/tests/mock/identityv1connect"
	mock_moduleloader "github.com/ponyruntime/pony/tests/mock/moduleloader"
	mock_modulev1connect "github.com/ponyruntime/pony/tests/mock/modulev1connect"
	"go.uber.org/mock/gomock"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/wippyai/module-registry-proto-go/registry/identity/v1"
	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

func TestManager_Load(t *testing.T) {
	tests := []struct {
		name         string
		dependency   deps.ManifestDependency
		module       *modulev1.Module
		labels       []*modulev1.Label
		download     *modulev1.DownloadResponse_Content
		wantCommitID string
		wantFilePath string
		wantContent  string
	}{
		{
			name: "range constraint",
			dependency: deps.ManifestDependency{
				Name: deps.Name{
					Organization: "test-org",
					Module:       "range-module",
				},
				Version: ">= v1.0.3 < v2.0.0",
			},
			module: &modulev1.Module{
				Id:   "range-module-id",
				Name: "range-module",
			},
			labels: []*modulev1.Label{
				{
					Id:       "range-label-1",
					ModuleId: "range-module-id",
					Name:     "v0.9.0",
					CommitId: "range-commit-1",
				},
				{
					Id:       "range-label-2",
					ModuleId: "range-module-id",
					Name:     "v1.0.2",
					CommitId: "range-commit-2",
				},
				{
					Id:       "range-label-3",
					ModuleId: "range-module-id",
					Name:     "v1.5.2", // This should be selected
					CommitId: "range-commit-3",
				},
				{
					Id:       "range-label-4",
					ModuleId: "range-module-id",
					Name:     "v2.0.0",
					CommitId: "range-commit-4",
				},
			},
			download: &modulev1.DownloadResponse_Content{
				Commit: &modulev1.Commit{
					Id: "range-commit-3",
				},
				Files: []*modulev1.File{
					{
						Path:    "module-range/range.txt",
						Content: []byte("range module content"),
					},
				},
			},
			wantCommitID: "range-commit-3",
			wantFilePath: "range.txt",
			wantContent:  "range module content",
		},
		{
			name: "upper bound constraint",
			dependency: deps.ManifestDependency{
				Name: deps.Name{
					Organization: "test-org",
					Module:       "upper-bound-module",
				},
				Version: "<v2.3.1",
			},
			module: &modulev1.Module{
				Id:   "upper-bound-module-id",
				Name: "upper-bound-module",
			},
			labels: []*modulev1.Label{
				{
					Id:       "upper-label-1",
					ModuleId: "upper-bound-module-id",
					Name:     "v1.0.0",
					CommitId: "upper-commit-1",
				},
				{
					Id:       "upper-label-2",
					ModuleId: "upper-bound-module-id",
					Name:     "v2.3.0", // This should be selected
					CommitId: "upper-commit-2",
				},
				{
					Id:       "upper-label-3",
					ModuleId: "upper-bound-module-id",
					Name:     "v2.3.1", // This exceeds the constraint
					CommitId: "upper-commit-3",
				},
			},
			download: &modulev1.DownloadResponse_Content{
				Commit: &modulev1.Commit{
					Id: "upper-commit-2",
				},
				Files: []*modulev1.File{
					{
						Path:    "module-upper/upper.txt",
						Content: []byte("upper bound module content"),
					},
				},
			},
			wantCommitID: "upper-commit-2",
			wantFilePath: "upper.txt",
			wantContent:  "upper bound module content",
		},
		{
			name: "exact version constraint with equals",
			dependency: deps.ManifestDependency{
				Name: deps.Name{
					Organization: "test-org",
					Module:       "exact-module",
				},
				Version: "=v0.3.9",
			},
			module: &modulev1.Module{
				Id:   "exact-module-id",
				Name: "exact-module",
			},
			labels: []*modulev1.Label{
				{
					Id:       "exact-label-1",
					ModuleId: "exact-module-id",
					Name:     "v0.3.8",
					CommitId: "exact-commit-1",
				},
				{
					Id:       "exact-label-2",
					ModuleId: "exact-module-id",
					Name:     "v0.3.9", // This should be selected
					CommitId: "exact-commit-2",
				},
				{
					Id:       "exact-label-3",
					ModuleId: "exact-module-id",
					Name:     "v0.3.10",
					CommitId: "exact-commit-3",
				},
			},
			download: &modulev1.DownloadResponse_Content{
				Commit: &modulev1.Commit{
					Id: "exact-commit-2",
				},
				Files: []*modulev1.File{
					{
						Path:    "module-exact/exact.txt",
						Content: []byte("exact module content"),
					},
				},
			},
			wantCommitID: "exact-commit-2",
			wantFilePath: "exact.txt",
			wantContent:  "exact module content",
		},
		{
			name: "implicit exact version",
			dependency: deps.ManifestDependency{
				Name: deps.Name{
					Organization: "test-org",
					Module:       "implicit-module",
				},
				Version: "v1.2.3",
			},
			module: &modulev1.Module{
				Id:   "implicit-module-id",
				Name: "implicit-module",
			},
			labels: []*modulev1.Label{
				{
					Id:       "implicit-label-1",
					ModuleId: "implicit-module-id",
					Name:     "v1.2.2",
					CommitId: "implicit-commit-1",
				},
				{
					Id:       "implicit-label-2",
					ModuleId: "implicit-module-id",
					Name:     "v1.2.3", // This should be selected
					CommitId: "implicit-commit-2",
				},
				{
					Id:       "implicit-label-3",
					ModuleId: "implicit-module-id",
					Name:     "v1.2.4",
					CommitId: "implicit-commit-3",
				},
			},
			download: &modulev1.DownloadResponse_Content{
				Commit: &modulev1.Commit{
					Id: "implicit-commit-2",
				},
				Files: []*modulev1.File{
					{
						Path:    "module-implicit/implicit.txt",
						Content: []byte("implicit module content"),
					},
				},
			},
			wantCommitID: "implicit-commit-2",
			wantFilePath: "implicit.txt",
			wantContent:  "implicit module content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks for this test case
			mockController := gomock.NewController(t)
			defer mockController.Finish()

			// Create a temporary directory for the test
			tempDir, err := os.MkdirTemp("", "moduleloader-test")
			require.NoError(t, err)
			defer os.RemoveAll(tempDir)

			// Create manifest with a single dependency for this test case
			testManifest := &deps.Manifest{
				Name:         "test-module",
				Dependencies: []deps.ManifestDependency{tt.dependency},
			}

			// Setup mocks
			mockManifestLoader := mock_moduleloader.NewMockManifestLoader(mockController)
			mockOrgClient := mock_identityv1connect.NewMockOrganizationServiceClient(mockController)
			mockModuleClient := mock_modulev1connect.NewMockModuleServiceClient(mockController)
			mockCommitClient := mock_modulev1connect.NewMockCommitServiceClient(mockController)
			mockLabelClient := mock_modulev1connect.NewMockLabelServiceClient(mockController)
			mockDownloadClient := mock_modulev1connect.NewMockDownloadServiceClient(mockController)

			// Configure mockManifestLoader
			mockManifestLoader.EXPECT().
				LoadManifest(gomock.Any()).
				Return(testManifest, nil)

			// Configure organization client mock
			mockOrgClient.EXPECT().
				ListOrganizations(gomock.Any(), gomock.Any()).
				Return(connect.NewResponse(&identityv1.ListOrganizationsResponse{
					Organizations: []*identityv1.Organization{
						{
							Id:   "org-123",
							Name: "test-org",
						},
					},
				}), nil)

			// Configure module client mock
			mockModuleClient.EXPECT().
				ListModules(gomock.Any(), gomock.Any()).
				Return(connect.NewResponse(&modulev1.ListModulesResponse{
					Modules: []*modulev1.Module{tt.module},
				}), nil)

			// Configure label client mock
			mockLabelClient.EXPECT().
				ListModuleLabels(gomock.Any(), gomock.Any()).
				Return(connect.NewResponse(&modulev1.ListModuleLabelsResponse{
					Labels: tt.labels,
				}), nil)

			// Configure download client mock
			mockDownloadClient.EXPECT().
				Download(gomock.Any(), gomock.Any()).
				Return(connect.NewResponse(&modulev1.DownloadResponse{
					Contents: []*modulev1.DownloadResponse_Content{tt.download},
				}), nil)

			// Create manager with all mocks and custom vendor folder
			vendorFolderPath := filepath.Join(tempDir, ".wippy")
			manager := deps.NewManager(
				mockOrgClient,
				mockModuleClient,
				mockCommitClient,
				mockLabelClient,
				mockDownloadClient,
				mockManifestLoader,
				vendorFolderPath,
			)

			// Call the Load method
			loadResult, err := manager.Load(context.Background())
			require.NoError(t, err)
			require.NotNil(t, loadResult)
			require.Len(t, loadResult.Modules, 1)

			// Verify the vendor folder was created
			_, err = os.Stat(vendorFolderPath)
			require.NoError(t, err)

			// Verify the module was downloaded with the correct version
			modulePath := filepath.Join(vendorFolderPath, tt.dependency.Name.Organization,
				tt.dependency.Name.Module+"@"+tt.wantCommitID)
			_, err = os.Stat(modulePath)
			require.NoError(t, err)

			// Find the module subdirectory
			entries, err := os.ReadDir(modulePath)
			require.NoError(t, err)

			var moduleSubdir string
			for _, entry := range entries {
				if entry.IsDir() && strings.HasPrefix(entry.Name(), "module-") {
					moduleSubdir = entry.Name()
					break
				}
			}
			require.NotEmpty(t, moduleSubdir, "Expected to find module subdirectory")

			content, err := os.ReadFile(filepath.Join(modulePath, moduleSubdir, tt.wantFilePath))
			require.NoError(t, err)
			assert.Equal(t, tt.wantContent, string(content))
		})
	}
}

func TestManager_Load_TreeDependencyProcessing(t *testing.T) {
	// Setup mocks
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "moduleloader-tree-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Setup mocks
	mockManifestLoader := mock_moduleloader.NewMockManifestLoader(mockController)
	mockOrgClient := mock_identityv1connect.NewMockOrganizationServiceClient(mockController)
	mockModuleClient := mock_modulev1connect.NewMockModuleServiceClient(mockController)
	mockCommitClient := mock_modulev1connect.NewMockCommitServiceClient(mockController)
	mockLabelClient := mock_modulev1connect.NewMockLabelServiceClient(mockController)
	mockDownloadClient := mock_modulev1connect.NewMockDownloadServiceClient(mockController)

	// Main dependency in manifest
	mainDep := deps.ManifestDependency{
		Name:    deps.Name{Organization: "test-org", Module: "main-module"},
		Version: "v1.0.0",
	}

	// Create manifest with main dependency only
	testManifest := &deps.Manifest{
		Name:         "test-module",
		Dependencies: []deps.ManifestDependency{mainDep},
	}

	// Configure manifest loader
	mockManifestLoader.EXPECT().
		LoadManifest(gomock.Any()).
		Return(testManifest, nil)

	// Configure organization client mock (called for both main and sub modules)
	mockOrgClient.EXPECT().
		ListOrganizations(gomock.Any(), gomock.Any()).
		Return(connect.NewResponse(&identityv1.ListOrganizationsResponse{
			Organizations: []*identityv1.Organization{
				{
					Id:   "org-123",
					Name: "test-org",
				},
			},
		}), nil).
		Times(2) // Called twice - once for main, once for sub dependency

	// Configure module client mock (called for both main and sub modules)
	mockModuleClient.EXPECT().
		ListModules(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error) {
			nameRef := req.Msg.Refs[0].GetNameRef()
			switch nameRef.Name {
			case "main-module":
				return connect.NewResponse(&modulev1.ListModulesResponse{
					Modules: []*modulev1.Module{
						{
							Id:   "main-module-id",
							Name: "main-module",
						},
					},
				}), nil
			case "sub-module":
				return connect.NewResponse(&modulev1.ListModulesResponse{
					Modules: []*modulev1.Module{
						{
							Id:   "sub-module-id",
							Name: "sub-module",
						},
					},
				}), nil
			}
			return nil, errors.New("unexpected module name")
		}).
		Times(2)

	// Configure label client mock (called for both main and sub modules)
	mockLabelClient.EXPECT().
		ListModuleLabels(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req *connect.Request[modulev1.ListModuleLabelsRequest]) (*connect.Response[modulev1.ListModuleLabelsResponse], error) {
			moduleID := req.Msg.ModuleIds[0]
			switch moduleID {
			case "main-module-id":
				return connect.NewResponse(&modulev1.ListModuleLabelsResponse{
					Labels: []*modulev1.Label{
						{
							Id:       "main-label-1",
							ModuleId: "main-module-id",
							Name:     "v1.0.0",
							CommitId: "main-commit-1",
						},
					},
				}), nil
			case "sub-module-id":
				return connect.NewResponse(&modulev1.ListModuleLabelsResponse{
					Labels: []*modulev1.Label{
						{
							Id:       "sub-label-1",
							ModuleId: "sub-module-id",
							Name:     "v1.1.0",
							CommitId: "sub-commit-1",
						},
					},
				}), nil
			}
			return nil, errors.New("unexpected module id")
		}).
		Times(2)

	// Configure download client mock (called for both main and sub modules)
	mockDownloadClient.EXPECT().
		Download(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req *connect.Request[modulev1.DownloadRequest]) (*connect.Response[modulev1.DownloadResponse], error) {
			commitID := req.Msg.CommitIds[0]
			switch commitID {
			case "main-commit-1":
				// Main module contains YAML file with sub-dependency
				yamlContent := `dependencies:
  - name: test-org/sub-module
    version: v1.1.0`
				return connect.NewResponse(&modulev1.DownloadResponse{
					Contents: []*modulev1.DownloadResponse_Content{
						{
							Commit: &modulev1.Commit{Id: "main-commit-1"},
							Files: []*modulev1.File{
								{
									Path:    "module-main/main.txt",
									Content: []byte("main module content"),
								},
								{
									Path:    "module-main/dependencies.yaml",
									Content: []byte(yamlContent),
								},
							},
						},
					},
				}), nil
			case "sub-commit-1":
				return connect.NewResponse(&modulev1.DownloadResponse{
					Contents: []*modulev1.DownloadResponse_Content{
						{
							Commit: &modulev1.Commit{Id: "sub-commit-1"},
							Files: []*modulev1.File{
								{
									Path:    "module-sub/sub.txt",
									Content: []byte("sub module content"),
								},
							},
						},
					},
				}), nil
			}
			return nil, errors.New("unexpected commit id")
		}).
		Times(2)

	// Create manager
	vendorFolderPath := filepath.Join(tempDir, ".wippy")
	manager := deps.NewManager(
		mockOrgClient,
		mockModuleClient,
		mockCommitClient,
		mockLabelClient,
		mockDownloadClient,
		mockManifestLoader,
		vendorFolderPath,
	)

	// Call the Load method
	loadResult, err := manager.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, loadResult)

	// Should have loaded both main and sub modules
	require.Len(t, loadResult.Modules, 2)

	// Verify both modules were downloaded
	mainModulePath := filepath.Join(vendorFolderPath, "test-org", "main-module@main-commit-1")
	subModulePath := filepath.Join(vendorFolderPath, "test-org", "sub-module@sub-commit-1")

	_, err = os.Stat(mainModulePath)
	require.NoError(t, err)

	_, err = os.Stat(subModulePath)
	require.NoError(t, err)

	// Verify module contents
	mainContent, err := os.ReadFile(filepath.Join(mainModulePath, "module-main", "main.txt"))
	require.NoError(t, err)
	assert.Equal(t, "main module content", string(mainContent))

	subContent, err := os.ReadFile(filepath.Join(subModulePath, "module-sub", "sub.txt"))
	require.NoError(t, err)
	assert.Equal(t, "sub module content", string(subContent))
}

func TestManager_Load_DuplicatePrevention(t *testing.T) {
	// Setup mocks
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "moduleloader-duplicate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Setup mocks
	mockManifestLoader := mock_moduleloader.NewMockManifestLoader(mockController)
	mockOrgClient := mock_identityv1connect.NewMockOrganizationServiceClient(mockController)
	mockModuleClient := mock_modulev1connect.NewMockModuleServiceClient(mockController)
	mockCommitClient := mock_modulev1connect.NewMockCommitServiceClient(mockController)
	mockLabelClient := mock_modulev1connect.NewMockLabelServiceClient(mockController)
	mockDownloadClient := mock_modulev1connect.NewMockDownloadServiceClient(mockController)

	// Shared dependency that appears in multiple places
	sharedDep := deps.ManifestDependency{
		Name:    deps.Name{Organization: "test-org", Module: "shared-module"},
		Version: "v1.0.0",
	}

	// Create manifest with the same dependency listed twice
	testManifest := &deps.Manifest{
		Name:         "test-module",
		Dependencies: []deps.ManifestDependency{sharedDep, sharedDep}, // Duplicate
	}

	// Configure manifest loader
	mockManifestLoader.EXPECT().
		LoadManifest(gomock.Any()).
		Return(testManifest, nil)

	// Configure organization client mock (should only be called once for unique dependencies)
	mockOrgClient.EXPECT().
		ListOrganizations(gomock.Any(), gomock.Any()).
		Return(connect.NewResponse(&identityv1.ListOrganizationsResponse{
			Organizations: []*identityv1.Organization{
				{
					Id:   "org-123",
					Name: "test-org",
				},
			},
		}), nil).
		Times(1) // Called only once despite duplicate dependency

	// Configure module client mock (should only be called once)
	mockModuleClient.EXPECT().
		ListModules(gomock.Any(), gomock.Any()).
		Return(connect.NewResponse(&modulev1.ListModulesResponse{
			Modules: []*modulev1.Module{
				{
					Id:   "shared-module-id",
					Name: "shared-module",
				},
			},
		}), nil).
		Times(1) // Called only once despite duplicate

	// Configure label client mock (should only be called once)
	mockLabelClient.EXPECT().
		ListModuleLabels(gomock.Any(), gomock.Any()).
		Return(connect.NewResponse(&modulev1.ListModuleLabelsResponse{
			Labels: []*modulev1.Label{
				{
					Id:       "shared-label-1",
					ModuleId: "shared-module-id",
					Name:     "v1.0.0",
					CommitId: "shared-commit-1",
				},
			},
		}), nil).
		Times(1) // Called only once despite duplicate

	// Configure download client mock (should only be called once)
	mockDownloadClient.EXPECT().
		Download(gomock.Any(), gomock.Any()).
		Return(connect.NewResponse(&modulev1.DownloadResponse{
			Contents: []*modulev1.DownloadResponse_Content{
				{
					Commit: &modulev1.Commit{Id: "shared-commit-1"},
					Files: []*modulev1.File{
						{
							Path:    "module-shared/shared.txt",
							Content: []byte("shared module content"),
						},
					},
				},
			},
		}), nil).
		Times(1) // Called only once despite duplicate

	// Create manager
	vendorFolderPath := filepath.Join(tempDir, ".wippy")
	manager := deps.NewManager(
		mockOrgClient,
		mockModuleClient,
		mockCommitClient,
		mockLabelClient,
		mockDownloadClient,
		mockManifestLoader,
		vendorFolderPath,
	)

	// Call the Load method
	loadResult, err := manager.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, loadResult)

	// Should have loaded only one instance of the shared module
	require.Len(t, loadResult.Modules, 1)

	// Verify module was downloaded only once
	sharedModulePath := filepath.Join(vendorFolderPath, "test-org", "shared-module@shared-commit-1")
	_, err = os.Stat(sharedModulePath)
	require.NoError(t, err)

	// Verify module content
	content, err := os.ReadFile(filepath.Join(sharedModulePath, "module-shared", "shared.txt"))
	require.NoError(t, err)
	assert.Equal(t, "shared module content", string(content))
}

func TestManager_Load_EntriesFormatDependencyScanning(t *testing.T) {
	// Setup mocks
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "moduleloader-entries-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Setup mocks
	mockManifestLoader := mock_moduleloader.NewMockManifestLoader(mockController)
	mockOrgClient := mock_identityv1connect.NewMockOrganizationServiceClient(mockController)
	mockModuleClient := mock_modulev1connect.NewMockModuleServiceClient(mockController)
	mockCommitClient := mock_modulev1connect.NewMockCommitServiceClient(mockController)
	mockLabelClient := mock_modulev1connect.NewMockLabelServiceClient(mockController)
	mockDownloadClient := mock_modulev1connect.NewMockDownloadServiceClient(mockController)

	// Main dependency in manifest
	mainDep := deps.ManifestDependency{
		Name:    deps.Name{Organization: "test-org", Module: "main-module"},
		Version: "v1.0.0",
	}

	// Create manifest with main dependency only
	testManifest := &deps.Manifest{
		Name:         "test-module",
		Dependencies: []deps.ManifestDependency{mainDep},
	}

	// Configure manifest loader
	mockManifestLoader.EXPECT().
		LoadManifest(gomock.Any()).
		Return(testManifest, nil)

	// Configure organization client mock (called for both main and entries-based dependency)
	mockOrgClient.EXPECT().
		ListOrganizations(gomock.Any(), gomock.Any()).
		Return(connect.NewResponse(&identityv1.ListOrganizationsResponse{
			Organizations: []*identityv1.Organization{
				{
					Id:   "org-123",
					Name: "test-org",
				},
			},
		}), nil).
		Times(2) // Called twice - once for main, once for entries dependency

	// Configure module client mock (called for both modules)
	mockModuleClient.EXPECT().
		ListModules(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error) {
			nameRef := req.Msg.Refs[0].GetNameRef()
			switch nameRef.Name {
			case "main-module":
				return connect.NewResponse(&modulev1.ListModulesResponse{
					Modules: []*modulev1.Module{
						{
							Id:   "main-module-id",
							Name: "main-module",
						},
					},
				}), nil
			case "entries-module":
				return connect.NewResponse(&modulev1.ListModulesResponse{
					Modules: []*modulev1.Module{
						{
							Id:   "entries-module-id",
							Name: "entries-module",
						},
					},
				}), nil
			}
			return nil, errors.New("unexpected module name")
		}).
		Times(2)

	// Configure label client mock (called for both modules)
	mockLabelClient.EXPECT().
		ListModuleLabels(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req *connect.Request[modulev1.ListModuleLabelsRequest]) (*connect.Response[modulev1.ListModuleLabelsResponse], error) {
			moduleID := req.Msg.ModuleIds[0]
			switch moduleID {
			case "main-module-id":
				return connect.NewResponse(&modulev1.ListModuleLabelsResponse{
					Labels: []*modulev1.Label{
						{
							Id:       "main-label-1",
							ModuleId: "main-module-id",
							Name:     "v1.0.0",
							CommitId: "main-commit-1",
						},
					},
				}), nil
			case "entries-module-id":
				return connect.NewResponse(&modulev1.ListModuleLabelsResponse{
					Labels: []*modulev1.Label{
						{
							Id:       "entries-label-1",
							ModuleId: "entries-module-id",
							Name:     "v2.0.0",
							CommitId: "entries-commit-1",
						},
					},
				}), nil
			}
			return nil, errors.New("unexpected module id")
		}).
		Times(2)

	// Configure download client mock (called for both modules)
	mockDownloadClient.EXPECT().
		Download(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req *connect.Request[modulev1.DownloadRequest]) (*connect.Response[modulev1.DownloadResponse], error) {
			commitID := req.Msg.CommitIds[0]
			switch commitID {
			case "main-commit-1":
				// Main module contains YAML file with entries format dependency
				entriesContent := `entries:
  - name: entry1
    kind: ns.dependency
    component: test-org/entries-module
    version: v2.0.0
  - name: entry2
    kind: other.kind
    component: test-org/other-module
    version: v1.0.0`
				return connect.NewResponse(&modulev1.DownloadResponse{
					Contents: []*modulev1.DownloadResponse_Content{
						{
							Commit: &modulev1.Commit{Id: "main-commit-1"},
							Files: []*modulev1.File{
								{
									Path:    "module-main/main.txt",
									Content: []byte("main module content"),
								},
								{
									Path:    "module-main/entries.yaml",
									Content: []byte(entriesContent),
								},
							},
						},
					},
				}), nil
			case "entries-commit-1":
				return connect.NewResponse(&modulev1.DownloadResponse{
					Contents: []*modulev1.DownloadResponse_Content{
						{
							Commit: &modulev1.Commit{Id: "entries-commit-1"},
							Files: []*modulev1.File{
								{
									Path:    "module-entries/entries.txt",
									Content: []byte("entries module content"),
								},
							},
						},
					},
				}), nil
			}
			return nil, errors.New("unexpected commit id")
		}).
		Times(2)

	// Create manager
	vendorFolderPath := filepath.Join(tempDir, ".wippy")
	manager := deps.NewManager(
		mockOrgClient,
		mockModuleClient,
		mockCommitClient,
		mockLabelClient,
		mockDownloadClient,
		mockManifestLoader,
		vendorFolderPath,
	)

	// Call the Load method
	loadResult, err := manager.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, loadResult)

	// Should have loaded both main and entries modules
	require.Len(t, loadResult.Modules, 2)

	// Verify both modules were downloaded
	mainModulePath := filepath.Join(vendorFolderPath, "test-org", "main-module@main-commit-1")
	entriesModulePath := filepath.Join(vendorFolderPath, "test-org", "entries-module@entries-commit-1")

	_, err = os.Stat(mainModulePath)
	require.NoError(t, err)

	_, err = os.Stat(entriesModulePath)
	require.NoError(t, err)

	// Verify module contents
	mainContent, err := os.ReadFile(filepath.Join(mainModulePath, "module-main", "main.txt"))
	require.NoError(t, err)
	assert.Equal(t, "main module content", string(mainContent))

	entriesContent, err := os.ReadFile(filepath.Join(entriesModulePath, "module-entries", "entries.txt"))
	require.NoError(t, err)
	assert.Equal(t, "entries module content", string(entriesContent))
}
