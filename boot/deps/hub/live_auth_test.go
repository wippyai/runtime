// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	manifestv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1"
	manifestv1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1/manifestv1connect"
	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
	modulev1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1/modulev1connect"
	versionv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/version/v1"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/auth"
)

type privateModuleService struct {
	modulev1connect.UnimplementedModuleServiceHandler
}

func (*privateModuleService) ListVersions(context.Context, *connect.Request[modulev1.ListVersionsRequest]) (*connect.Response[modulev1.ListVersionsResponse], error) {
	return connect.NewResponse(&modulev1.ListVersionsResponse{
		Versions: []*versionv1.Version{{Id: "release-1", Version: "1.0.0"}},
	}), nil
}

type privateManifestService struct {
	manifestv1connect.UnimplementedManifestServiceHandler
}

func (*privateManifestService) GetManifest(context.Context, *connect.Request[manifestv1.GetManifestRequest]) (*connect.Response[manifestv1.GetManifestResponse], error) {
	return connect.NewResponse(&manifestv1.GetManifestResponse{Manifest: &manifestv1.ModuleManifest{
		Org: "acme", Name: "private", Version: "1.0.0", VersionId: "release-1",
		Digest: "sha256:" + strings.Repeat("a", 64),
	}}), nil
}

func TestDependencyHandlerUsesCurrentCredentialsAfterStartup(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv(auth.EnvToken, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	var expected atomic.Value
	expected.Store("Bearer test-first-login")
	mux := http.NewServeMux()
	path, handler := modulev1connect.NewModuleServiceHandler(&privateModuleService{})
	mux.Handle(path, handler)
	path, handler = manifestv1connect.NewManifestServiceHandler(&privateManifestService{})
	mux.Handle(path, handler)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != expected.Load().(string) {
			connect.NewErrorWriter().Write(w, r, connect.NewError(connect.CodeNotFound, errors.New("module not found")))
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	t.Setenv(auth.EnvRegistry, server.URL)
	t.Cleanup(func() { auth.ClearRuntimeToken(server.URL) })

	// The same handler outlives login, token rotation and logout, as it does in
	// a running registry. All requests use an actual Connect server.
	deps, err := NewDependencyHandler(DependencyHandlerOptions{VendorDir: filepath.Join(root, "vendor")})
	require.NoError(t, err)
	t.Cleanup(deps.manifestCache.Close)
	ctx := regapi.WithDependencyAccess(t.Context(), regapi.DependencyAccessOnline)
	requests := []DependencyDefinition{{Component: "acme/private", Version: ">=1.0.0"}}
	_, err = deps.ResolveWorkspaceDependencies(ctx, requests)
	require.ErrorContains(t, err, "module not found")

	// UI authentication writes this exact runtime credential override. A fresh
	// catalog client already sees it; the existing install handler must too.
	auth.SetRuntimeToken(server.URL, "test-first-login")
	catalog, err := NewClientFromAuth("")
	require.NoError(t, err)
	versions, err := catalog.ListAllVersions(t.Context(), "acme", "private")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	modules, err := deps.ResolveWorkspaceDependencies(ctx, requests)
	require.NoError(t, err, "login after startup must authenticate the existing install handler")
	require.Len(t, modules, 1)

	expected.Store("Bearer test-rotated-login")
	auth.SetRuntimeToken(server.URL, "test-rotated-login")
	_, err = deps.ResolveWorkspaceDependencies(ctx, requests)
	require.NoError(t, err, "the next request must use the rotated credential")

	auth.ClearRuntimeToken(server.URL)
	_, err = deps.ResolveWorkspaceDependencies(ctx, requests)
	require.ErrorContains(t, err, "module not found", "logout must stop sending the previous credential")

	otherRegistry := server.URL + "/other-registry"
	auth.SetRuntimeToken(otherRegistry, "test-rotated-login")
	t.Cleanup(func() { auth.ClearRuntimeToken(otherRegistry) })
	_, err = deps.ResolveWorkspaceDependencies(ctx, requests)
	require.ErrorContains(t, err, "module not found", "another registry's credential must not be used")

	// Logout clears the runtime override, retaining the store's existing
	// environment/file fallback semantics.
	t.Setenv(auth.EnvToken, "test-environment")
	expected.Store("Bearer test-environment")
	_, err = deps.ResolveWorkspaceDependencies(ctx, requests)
	require.NoError(t, err)
	auth.SetRuntimeToken(server.URL, "test-runtime-override")
	expected.Store("Bearer test-runtime-override")
	_, err = deps.ResolveWorkspaceDependencies(ctx, requests)
	require.NoError(t, err)
	auth.ClearRuntimeToken(server.URL)
	expected.Store("Bearer test-environment")
	_, err = deps.ResolveWorkspaceDependencies(ctx, requests)
	require.NoError(t, err)
}

func TestExplicitRegistryClientUsesCurrentCredentials(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(auth.EnvToken, "")
	t.Setenv(auth.EnvRegistry, "https://default.invalid")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	var expected atomic.Value
	expected.Store("")
	_, handler := modulev1connect.NewModuleServiceHandler(&privateModuleService{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, expected.Load().(string), r.Header.Get("Authorization"))
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	t.Cleanup(func() { auth.ClearRuntimeToken(server.URL) })
	client, err := NewClientFromAuth(server.URL)
	require.NoError(t, err)
	for _, token := range []string{"", "first-login", "rotated-login", ""} {
		if token == "" {
			auth.ClearRuntimeToken(server.URL)
			expected.Store("")
		} else {
			auth.SetRuntimeToken(server.URL, token)
			expected.Store("Bearer " + token)
		}
		versions, err := client.ListAllVersions(t.Context(), "acme", "private")
		require.NoError(t, err)
		require.Len(t, versions, 1)
	}
}
