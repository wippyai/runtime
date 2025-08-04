package moduleloader_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/moduleloader"
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
		dependency   moduleloader.ManifestDependency
		module       *modulev1.Module
		labels       []*modulev1.Label
		download     *modulev1.DownloadResponse_Content
		wantCommitID string
		wantFilePath string
		wantContent  string
	}{
		{
			name: "range constraint",
			dependency: moduleloader.ManifestDependency{
				Name: moduleloader.Name{
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
						Path:    "range.txt",
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
			dependency: moduleloader.ManifestDependency{
				Name: moduleloader.Name{
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
						Path:    "upper.txt",
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
			dependency: moduleloader.ManifestDependency{
				Name: moduleloader.Name{
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
						Path:    "exact.txt",
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
			dependency: moduleloader.ManifestDependency{
				Name: moduleloader.Name{
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
						Path:    "implicit.txt",
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
			testManifest := &moduleloader.Manifest{
				Name:         "test-module",
				Dependencies: []moduleloader.ManifestDependency{tt.dependency},
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
			manager := moduleloader.NewManager(
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

			content, err := os.ReadFile(filepath.Join(modulePath, tt.wantFilePath))
			require.NoError(t, err)
			assert.Equal(t, tt.wantContent, string(content))
		})
	}
}
