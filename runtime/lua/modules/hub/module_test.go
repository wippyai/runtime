// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
	modulev1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1/modulev1connect"
	versionv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/version/v1"
	secapi "github.com/wippyai/runtime/api/security"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	boothub "github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/wapp"
)

type fakeModuleClient struct {
	listModulesFn   func(context.Context, *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error)
	searchModulesFn func(context.Context, *connect.Request[modulev1.SearchModulesRequest]) (*connect.Response[modulev1.SearchModulesResponse], error)
	getModuleFn     func(context.Context, *connect.Request[modulev1.GetModuleRequest]) (*connect.Response[modulev1.GetModuleResponse], error)
	listVersionsFn  func(context.Context, *connect.Request[modulev1.ListVersionsRequest]) (*connect.Response[modulev1.ListVersionsResponse], error)
	getVersionFn    func(context.Context, *connect.Request[modulev1.GetVersionRequest]) (*connect.Response[modulev1.GetVersionResponse], error)
	listFilesFn     func(context.Context, *connect.Request[modulev1.ListVersionFilesRequest]) (*connect.Response[modulev1.ListVersionFilesResponse], error)
	getDepsFn       func(context.Context, *connect.Request[modulev1.GetDependenciesRequest]) (*connect.Response[modulev1.GetDependenciesResponse], error)
	getDependentsFn func(context.Context, *connect.Request[modulev1.GetDependentsRequest]) (*connect.Response[modulev1.GetDependentsResponse], error)
	getReadmeFn     func(context.Context, *connect.Request[modulev1.GetReadmeRequest]) (*connect.Response[modulev1.GetReadmeResponse], error)
}

func (f *fakeModuleClient) ListModules(ctx context.Context, req *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error) {
	if f.listModulesFn != nil {
		return f.listModulesFn(ctx, req)
	}
	return connect.NewResponse(&modulev1.ListModulesResponse{}), nil
}

func (f *fakeModuleClient) CreateModule(_ context.Context, _ *connect.Request[modulev1.CreateModuleRequest]) (*connect.Response[modulev1.CreateModuleResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (f *fakeModuleClient) UpdateModule(_ context.Context, _ *connect.Request[modulev1.UpdateModuleRequest]) (*connect.Response[modulev1.UpdateModuleResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (f *fakeModuleClient) DeleteModule(_ context.Context, _ *connect.Request[modulev1.DeleteModuleRequest]) (*connect.Response[modulev1.DeleteModuleResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (f *fakeModuleClient) DeprecateModule(_ context.Context, _ *connect.Request[modulev1.DeprecateModuleRequest]) (*connect.Response[modulev1.DeprecateModuleResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (f *fakeModuleClient) SearchModules(ctx context.Context, req *connect.Request[modulev1.SearchModulesRequest]) (*connect.Response[modulev1.SearchModulesResponse], error) {
	if f.searchModulesFn != nil {
		return f.searchModulesFn(ctx, req)
	}
	return connect.NewResponse(&modulev1.SearchModulesResponse{}), nil
}

func (f *fakeModuleClient) GetModule(ctx context.Context, req *connect.Request[modulev1.GetModuleRequest]) (*connect.Response[modulev1.GetModuleResponse], error) {
	if f.getModuleFn != nil {
		return f.getModuleFn(ctx, req)
	}
	return connect.NewResponse(&modulev1.GetModuleResponse{}), nil
}

func (f *fakeModuleClient) ListVersions(ctx context.Context, req *connect.Request[modulev1.ListVersionsRequest]) (*connect.Response[modulev1.ListVersionsResponse], error) {
	if f.listVersionsFn != nil {
		return f.listVersionsFn(ctx, req)
	}
	return connect.NewResponse(&modulev1.ListVersionsResponse{}), nil
}

func (f *fakeModuleClient) GetVersion(ctx context.Context, req *connect.Request[modulev1.GetVersionRequest]) (*connect.Response[modulev1.GetVersionResponse], error) {
	if f.getVersionFn != nil {
		return f.getVersionFn(ctx, req)
	}
	return connect.NewResponse(&modulev1.GetVersionResponse{}), nil
}

func (f *fakeModuleClient) ListVersionFiles(ctx context.Context, req *connect.Request[modulev1.ListVersionFilesRequest]) (*connect.Response[modulev1.ListVersionFilesResponse], error) {
	if f.listFilesFn != nil {
		return f.listFilesFn(ctx, req)
	}
	return connect.NewResponse(&modulev1.ListVersionFilesResponse{}), nil
}

func (f *fakeModuleClient) GetDependencies(ctx context.Context, req *connect.Request[modulev1.GetDependenciesRequest]) (*connect.Response[modulev1.GetDependenciesResponse], error) {
	if f.getDepsFn != nil {
		return f.getDepsFn(ctx, req)
	}
	return connect.NewResponse(&modulev1.GetDependenciesResponse{}), nil
}

func (f *fakeModuleClient) GetDependents(ctx context.Context, req *connect.Request[modulev1.GetDependentsRequest]) (*connect.Response[modulev1.GetDependentsResponse], error) {
	if f.getDependentsFn != nil {
		return f.getDependentsFn(ctx, req)
	}
	return connect.NewResponse(&modulev1.GetDependentsResponse{}), nil
}

func (f *fakeModuleClient) GetReadme(ctx context.Context, req *connect.Request[modulev1.GetReadmeRequest]) (*connect.Response[modulev1.GetReadmeResponse], error) {
	if f.getReadmeFn != nil {
		return f.getReadmeFn(ctx, req)
	}
	return connect.NewResponse(&modulev1.GetReadmeResponse{}), nil
}

var _ modulev1connect.ModuleServiceClient = (*fakeModuleClient)(nil)

type fakeArtifactClient struct {
	getDownloadFn func(context.Context, *boothub.DownloadParams) (*boothub.DownloadInfo, error)
	downloadFn    func(context.Context, string, string) error
}

func (f *fakeArtifactClient) GetDownloadURL(ctx context.Context, params *boothub.DownloadParams) (*boothub.DownloadInfo, error) {
	if f.getDownloadFn != nil {
		return f.getDownloadFn(ctx, params)
	}
	return &boothub.DownloadInfo{URL: "memory://artifact"}, nil
}

func (f *fakeArtifactClient) DownloadToFile(ctx context.Context, url, destPath string) error {
	if f.downloadFn != nil {
		return f.downloadFn(ctx, url, destPath)
	}
	return errors.New("download not implemented")
}

func setupContext() context.Context {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx = secapi.SetStrictMode(ctx, false)
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestModulesList(t *testing.T) {
	fake := &fakeModuleClient{}
	fake.listModulesFn = func(_ context.Context, req *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error) {
		if req.Msg.GetPage() != 2 {
			return nil, errors.New("unexpected page")
		}
		if req.Msg.GetPageSize() != 5 {
			return nil, errors.New("unexpected page_size")
		}
		resp := &modulev1.ListModulesResponse{
			Modules: []*modulev1.Module{{
				Id:               "mod_1",
				Name:             "terminal",
				OrganizationName: "wippy",
				LatestVersion:    "1.2.3",
			}},
			Total:    1,
			Page:     2,
			PageSize: 5,
		}
		return connect.NewResponse(resp), nil
	}

	mod := NewModule(Options{ModuleClient: fake})
	l := lua.NewState()
	defer l.Close()
	l.SetContext(setupContext())

	tbl, _ := mod.Build()
	l.SetGlobal(mod.Name, tbl)

	if err := l.DoString(`
		local res, err = hub.modules.list({page = 2, page_size = 5})
		if err then error(err) end
		if res.page ~= 2 then error("page mismatch") end
		if res.page_size ~= 5 then error("page_size mismatch") end
		if res.total ~= 1 then error("total mismatch") end
		if res.items[1].full_name ~= "wippy/terminal" then error("full_name mismatch") end
		if res.items[1].latest_version ~= "1.2.3" then error("version mismatch") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestVersionsGetRequiresVersion(t *testing.T) {
	fake := &fakeModuleClient{}
	mod := NewModule(Options{ModuleClient: fake})
	l := lua.NewState()
	defer l.Close()
	l.SetContext(setupContext())

	tbl, _ := mod.Build()
	l.SetGlobal(mod.Name, tbl)

	if err := l.DoString(`
		local _, err = hub.versions.get("wippy/terminal")
		if err == nil then error("expected error") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestVersionsInspectExtractsRequirementsFromArtifact(t *testing.T) {
	t.Chdir(t.TempDir())
	artifact := buildWappBytesForHubModuleTest(t, []wapp.Entry{
		{
			ID:   wapp.NewID("wippy.dummy", "router"),
			Kind: "ns.requirement",
			Meta: wapp.Metadata{"description": "Router to register endpoints on"},
			Data: map[string]any{
				"default": "app:router",
				"targets": []any{
					map[string]any{"entry": "wippy.dummy:ping", "path": "meta.router"},
				},
			},
		},
		{
			ID:   wapp.NewID("wippy.dummy", "ping"),
			Kind: "function.lua",
		},
	})

	var requested *boothub.DownloadParams
	var downloads int
	fake := &fakeArtifactClient{
		getDownloadFn: func(_ context.Context, params *boothub.DownloadParams) (*boothub.DownloadInfo, error) {
			requested = params
			return &boothub.DownloadInfo{
				URL:     "memory://dummy",
				Version: "v0.1.2",
			}, nil
		},
		downloadFn: func(_ context.Context, url, destPath string) error {
			downloads++
			require.Equal(t, "memory://dummy", url)
			return os.WriteFile(destPath, artifact, 0600)
		},
	}

	mod := NewModule(Options{ArtifactClient: fake})
	l := lua.NewState()
	defer l.Close()
	l.SetContext(setupContext())

	tbl, _ := mod.Build()
	l.SetGlobal(mod.Name, tbl)

	if err := l.DoString(`
		local res, err = hub.versions.inspect("wippy/dummy", "v0.1.2")
		if err then error(err) end
		if res.version ~= "v0.1.2" then error("version mismatch") end
		if res.entry_count ~= 2 then error("entry_count mismatch") end
		local cache_path = string.gsub(res.cache_path, "\\", "/")
		if cache_path ~= ".wippy/vendor/wippy/dummy-v0.1.2.wapp" then error("cache path mismatch: " .. tostring(res.cache_path)) end
		if res.requirements[1].name ~= "router" then error("requirement name mismatch") end
		if res.requirements[1].description ~= "Router to register endpoints on" then error("description mismatch") end
		if res.requirements[1].default ~= "app:router" then error("default mismatch") end
		if res.requirements[1].targets[1].entry ~= "wippy.dummy:ping" then error("target entry mismatch") end
		if res.requirements[1].targets[1].path ~= "meta.router" then error("target path mismatch") end
		local cached, cached_err = hub.versions.inspect("wippy/dummy", "v0.1.2")
		if cached_err then error(cached_err) end
		if cached.requirements[1].name ~= "router" then error("cached requirement mismatch") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}

	require.Equal(t, 1, downloads)
	require.NotNil(t, requested)
	require.Equal(t, "wippy", requested.Org)
	require.Equal(t, "dummy", requested.Module)
	require.Equal(t, "v0.1.2", requested.Version)
}

func TestDependenciesGetOptionalVersion(t *testing.T) {
	fake := &fakeModuleClient{}
	fake.getDepsFn = func(_ context.Context, req *connect.Request[modulev1.GetDependenciesRequest]) (*connect.Response[modulev1.GetDependenciesResponse], error) {
		deps := []*versionv1.Dependency{{Org: "wippy", Name: "core", VersionConstraint: ">=1.0.0"}}
		return connect.NewResponse(&modulev1.GetDependenciesResponse{Dependencies: deps}), nil
	}

	mod := NewModule(Options{ModuleClient: fake})
	l := lua.NewState()
	defer l.Close()
	l.SetContext(setupContext())

	tbl, _ := mod.Build()
	l.SetGlobal(mod.Name, tbl)

	if err := l.DoString(`
		local res, err = hub.dependencies.get("wippy/terminal")
		if err then error(err) end
		if res.items[1].name ~= "core" then error("dependency mismatch") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestHubModule_ModuleClientShortCircuitDoesNotInitializeStore(t *testing.T) {
	h := newHubModule(Options{
		ModuleClient: &fakeModuleClient{},
	})

	l := lua.NewState()
	defer l.Close()
	l.SetContext(setupContext())

	_, err := h.moduleClient(l, baseOptions{})
	require.Nil(t, err)
	assert.Nil(t, h.store)
}

func buildWappBytesForHubModuleTest(t *testing.T, entries []wapp.Entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := wapp.NewWriter()
	require.NoError(t, writer.PackEntries(wapp.Metadata{}, entries, &buf))
	return buf.Bytes()
}

func TestHubModule_UsesProvidedAuthStoreLazily(t *testing.T) {
	store := bootauth.NewStore(bootauth.NewConfig(t.TempDir()))
	h := newHubModule(Options{
		AuthStore: store,
	})

	assert.Nil(t, h.store)
	got := h.authStore()
	assert.Same(t, store, got)
	assert.Same(t, store, h.store)
}
