package client

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	identityv1 "github.com/wippyai/module-registry-proto-go/registry/identity/v1"
	"github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
)

type mockOrganizationClient struct {
	identityv1connect.OrganizationServiceClient
	listFn func(context.Context, *connect.Request[identityv1.ListOrganizationsRequest]) (*connect.Response[identityv1.ListOrganizationsResponse], error)
}

func (m *mockOrganizationClient) ListOrganizations(ctx context.Context, req *connect.Request[identityv1.ListOrganizationsRequest]) (*connect.Response[identityv1.ListOrganizationsResponse], error) {
	return m.listFn(ctx, req)
}

type mockModuleClient struct {
	modulev1connect.ModuleServiceClient
	listFn func(context.Context, *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error)
}

func (m *mockModuleClient) ListModules(ctx context.Context, req *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error) {
	return m.listFn(ctx, req)
}

type mockLabelClient struct {
	modulev1connect.LabelServiceClient
	listFn func(context.Context, *connect.Request[modulev1.ListModuleLabelsRequest]) (*connect.Response[modulev1.ListModuleLabelsResponse], error)
}

func (m *mockLabelClient) ListModuleLabels(ctx context.Context, req *connect.Request[modulev1.ListModuleLabelsRequest]) (*connect.Response[modulev1.ListModuleLabelsResponse], error) {
	return m.listFn(ctx, req)
}

type mockDownloadClient struct {
	modulev1connect.DownloadServiceClient
	downloadFn func(context.Context, *connect.Request[modulev1.DownloadRequest]) (*connect.Response[modulev1.DownloadResponse], error)
}

func (m *mockDownloadClient) Download(ctx context.Context, req *connect.Request[modulev1.DownloadRequest]) (*connect.Response[modulev1.DownloadResponse], error) {
	return m.downloadFn(ctx, req)
}

func TestGetOrganizations(t *testing.T) {
	t.Run("fetches organizations successfully", func(t *testing.T) {
		orgClient := &mockOrganizationClient{
			listFn: func(_ context.Context, _ *connect.Request[identityv1.ListOrganizationsRequest]) (*connect.Response[identityv1.ListOrganizationsResponse], error) {
				return connect.NewResponse(&identityv1.ListOrganizationsResponse{
					Organizations: []*identityv1.Organization{
						{Id: "org1", Name: "acme"},
						{Id: "org2", Name: "demo"},
					},
				}), nil
			},
		}

		client := NewRegistryClient(orgClient, nil, nil, nil)
		result, err := client.GetOrganizations(context.Background(), []string{"acme", "demo"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result))
		}
		if result[0].Name != "acme" || result[0].Organization.GetId() != "org1" {
			t.Errorf("unexpected result[0]: %+v", result[0])
		}
		if result[1].Name != "demo" || result[1].Organization.GetId() != "org2" {
			t.Errorf("unexpected result[1]: %+v", result[1])
		}
	})

	t.Run("returns nil for empty input", func(t *testing.T) {
		client := NewRegistryClient(nil, nil, nil, nil)
		result, err := client.GetOrganizations(context.Background(), []string{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})

	t.Run("returns error if organization not found", func(t *testing.T) {
		orgClient := &mockOrganizationClient{
			listFn: func(_ context.Context, _ *connect.Request[identityv1.ListOrganizationsRequest]) (*connect.Response[identityv1.ListOrganizationsResponse], error) {
				return connect.NewResponse(&identityv1.ListOrganizationsResponse{
					Organizations: []*identityv1.Organization{
						{Id: "org1", Name: "acme"},
					},
				}), nil
			},
		}

		client := NewRegistryClient(orgClient, nil, nil, nil)
		_, err := client.GetOrganizations(context.Background(), []string{"acme", "missing"})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("propagates API error", func(t *testing.T) {
		apiErr := errors.New("API failure")
		orgClient := &mockOrganizationClient{
			listFn: func(_ context.Context, _ *connect.Request[identityv1.ListOrganizationsRequest]) (*connect.Response[identityv1.ListOrganizationsResponse], error) {
				return nil, apiErr
			},
		}

		client := NewRegistryClient(orgClient, nil, nil, nil)
		_, err := client.GetOrganizations(context.Background(), []string{"acme"})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, apiErr) {
			t.Errorf("expected wrapped API error, got: %v", err)
		}
	})
}

func TestGetModules(t *testing.T) {
	t.Run("fetches modules successfully", func(t *testing.T) {
		moduleClient := &mockModuleClient{
			listFn: func(_ context.Context, _ *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error) {
				return connect.NewResponse(&modulev1.ListModulesResponse{
					Modules: []*modulev1.Module{
						{Id: "mod1", Name: "http"},
						{Id: "mod2", Name: "sql"},
					},
				}), nil
			},
		}

		client := NewRegistryClient(nil, moduleClient, nil, nil)
		result, err := client.GetModules(context.Background(), []ModuleInfo{
			{OrganizationID: "org1", Name: "http"},
			{OrganizationID: "org1", Name: "sql"},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result))
		}
		if result[0].Module.GetId() != "mod1" {
			t.Errorf("unexpected module ID: %s", result[0].Module.GetId())
		}
	})

	t.Run("returns nil for empty input", func(t *testing.T) {
		client := NewRegistryClient(nil, nil, nil, nil)
		result, err := client.GetModules(context.Background(), []ModuleInfo{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})

	t.Run("returns error if module not found", func(t *testing.T) {
		moduleClient := &mockModuleClient{
			listFn: func(_ context.Context, _ *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error) {
				return connect.NewResponse(&modulev1.ListModulesResponse{
					Modules: []*modulev1.Module{
						{Id: "mod1", Name: "http"},
					},
				}), nil
			},
		}

		client := NewRegistryClient(nil, moduleClient, nil, nil)
		_, err := client.GetModules(context.Background(), []ModuleInfo{
			{OrganizationID: "org1", Name: "http"},
			{OrganizationID: "org1", Name: "missing"},
		})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("propagates API error", func(t *testing.T) {
		apiErr := errors.New("API failure")
		moduleClient := &mockModuleClient{
			listFn: func(_ context.Context, _ *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error) {
				return nil, apiErr
			},
		}

		client := NewRegistryClient(nil, moduleClient, nil, nil)
		_, err := client.GetModules(context.Background(), []ModuleInfo{
			{OrganizationID: "org1", Name: "http"},
		})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, apiErr) {
			t.Errorf("expected wrapped API error, got: %v", err)
		}
	})
}

func TestGetLabels(t *testing.T) {
	t.Run("fetches labels successfully", func(t *testing.T) {
		labelClient := &mockLabelClient{
			listFn: func(_ context.Context, _ *connect.Request[modulev1.ListModuleLabelsRequest]) (*connect.Response[modulev1.ListModuleLabelsResponse], error) {
				return connect.NewResponse(&modulev1.ListModuleLabelsResponse{
					Labels: []*modulev1.Label{
						{Id: "lbl1", ModuleId: "mod1", Name: "v1.0.0", CommitId: "commit1"},
						{Id: "lbl2", ModuleId: "mod1", Name: "v1.1.0", CommitId: "commit2"},
						{Id: "lbl3", ModuleId: "mod2", Name: "v2.0.0", CommitId: "commit3"},
					},
				}), nil
			},
		}

		client := NewRegistryClient(nil, nil, labelClient, nil)
		result, err := client.GetLabels(context.Background(), []string{"mod1", "mod2"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result))
		}
		if len(result[0].Labels) != 2 {
			t.Errorf("expected 2 labels for mod1, got %d", len(result[0].Labels))
		}
		if len(result[1].Labels) != 1 {
			t.Errorf("expected 1 label for mod2, got %d", len(result[1].Labels))
		}
	})

	t.Run("returns nil for empty input", func(t *testing.T) {
		client := NewRegistryClient(nil, nil, nil, nil)
		result, err := client.GetLabels(context.Background(), []string{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})

	t.Run("returns error if module has no labels", func(t *testing.T) {
		labelClient := &mockLabelClient{
			listFn: func(_ context.Context, _ *connect.Request[modulev1.ListModuleLabelsRequest]) (*connect.Response[modulev1.ListModuleLabelsResponse], error) {
				return connect.NewResponse(&modulev1.ListModuleLabelsResponse{
					Labels: []*modulev1.Label{
						{Id: "lbl1", ModuleId: "mod1", Name: "v1.0.0"},
					},
				}), nil
			},
		}

		client := NewRegistryClient(nil, nil, labelClient, nil)
		_, err := client.GetLabels(context.Background(), []string{"mod1", "mod2"})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("propagates API error", func(t *testing.T) {
		apiErr := errors.New("API failure")
		labelClient := &mockLabelClient{
			listFn: func(_ context.Context, _ *connect.Request[modulev1.ListModuleLabelsRequest]) (*connect.Response[modulev1.ListModuleLabelsResponse], error) {
				return nil, apiErr
			},
		}

		client := NewRegistryClient(nil, nil, labelClient, nil)
		_, err := client.GetLabels(context.Background(), []string{"mod1"})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, apiErr) {
			t.Errorf("expected wrapped API error, got: %v", err)
		}
	})
}

func TestDownload(t *testing.T) {
	t.Run("downloads content successfully", func(t *testing.T) {
		downloadClient := &mockDownloadClient{
			downloadFn: func(_ context.Context, _ *connect.Request[modulev1.DownloadRequest]) (*connect.Response[modulev1.DownloadResponse], error) {
				return connect.NewResponse(&modulev1.DownloadResponse{
					Contents: []*modulev1.DownloadResponse_Content{
						{
							Commit: &modulev1.Commit{Id: "commit1"},
							Files: []*modulev1.File{
								{Path: "main.lua", Content: []byte("content1")},
							},
						},
						{
							Commit: &modulev1.Commit{Id: "commit2"},
							Files: []*modulev1.File{
								{Path: "init.lua", Content: []byte("content2")},
							},
						},
					},
				}), nil
			},
		}

		client := NewRegistryClient(nil, nil, nil, downloadClient)
		result, err := client.Download(context.Background(), []string{"commit1", "commit2"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result))
		}
		if result[0].CommitID != "commit1" {
			t.Errorf("unexpected commit ID: %s", result[0].CommitID)
		}
		if len(result[0].Files) != 1 {
			t.Errorf("expected 1 file, got %d", len(result[0].Files))
		}
	})

	t.Run("returns nil for empty input", func(t *testing.T) {
		client := NewRegistryClient(nil, nil, nil, nil)
		result, err := client.Download(context.Background(), []string{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})

	t.Run("returns error if no content downloaded", func(t *testing.T) {
		downloadClient := &mockDownloadClient{
			downloadFn: func(_ context.Context, _ *connect.Request[modulev1.DownloadRequest]) (*connect.Response[modulev1.DownloadResponse], error) {
				return connect.NewResponse(&modulev1.DownloadResponse{
					Contents: []*modulev1.DownloadResponse_Content{},
				}), nil
			},
		}

		client := NewRegistryClient(nil, nil, nil, downloadClient)
		_, err := client.Download(context.Background(), []string{"commit1"})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("propagates API error", func(t *testing.T) {
		apiErr := errors.New("API failure")
		downloadClient := &mockDownloadClient{
			downloadFn: func(_ context.Context, _ *connect.Request[modulev1.DownloadRequest]) (*connect.Response[modulev1.DownloadResponse], error) {
				return nil, apiErr
			},
		}

		client := NewRegistryClient(nil, nil, nil, downloadClient)
		_, err := client.Download(context.Background(), []string{"commit1"})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, apiErr) {
			t.Errorf("expected wrapped API error, got: %v", err)
		}
	})
}
