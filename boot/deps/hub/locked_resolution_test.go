// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

const lockedResolutionDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func lockedResolutionHandler(t *testing.T, modules []lock.Module) *DependencyHandler {
	t.Helper()
	lockObj, err := lock.New(filepath.Join(t.TempDir(), "wippy.lock"))
	require.NoError(t, err)
	lockObj.SetDirectories(lock.Directories{Modules: ".wippy", Src: "src"})
	lockObj.ReplaceModules(modules)
	return &DependencyHandler{
		lock:         lockObj,
		logger:       zap.NewNop(),
		replacements: make(map[string]lock.Replacement),
	}
}

func TestLockedResolutionSkipsResolverForExactDeployment(t *testing.T) {
	handler := lockedResolutionHandler(t, []lock.Module{
		{Name: "acme/app", Version: "1.2.3", Hash: lockedResolutionDigest},
		{Name: "acme/lib", Version: "2.0.0", Hash: "sha256:" + lockedResolutionDigest},
	})

	resolved, ok := handler.lockedResolution(
		[]DependencyDefinition{{Component: "acme/app", Version: ">=1.0.0"}},
		map[string]string{"acme/app": "1.2.3", "acme/lib": "2.0.0"},
	)
	require.True(t, ok)
	require.Len(t, resolved, 2)
	require.Equal(t, "acme/app", resolved[0].Org+"/"+resolved[0].Name)
	require.Equal(t, "sha256:"+lockedResolutionDigest, resolved[0].Digest)
}

func TestResolveEffectiveModulesDoesNotCallHubForExactLock(t *testing.T) {
	handler := lockedResolutionHandler(t, []lock.Module{
		{Name: "acme/app", Version: "1.2.3", Hash: lockedResolutionDigest},
	})
	handler.hub = &fakeHub{
		getManifest: func(context.Context, string, string, string) (*ModuleManifest, error) {
			t.Fatal("exact locked deployment must not call the Hub resolver")
			return nil, nil
		},
	}
	handler.manifestCache = NewManifestCache(handler.hub)
	t.Cleanup(handler.manifestCache.Close)

	ctx := regapi.WithDependencyAccess(context.Background(), regapi.DependencyAccessVerifiedOffline)
	resolved, err := handler.resolveEffectiveModules(
		ctx,
		[]DependencyDefinition{{Component: "acme/app", Version: "1.2.3"}},
		map[string]string{"acme/app": "1.2.3"},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
}

func TestLockedResolutionRejectsIncompleteOrDriftedEvidence(t *testing.T) {
	tests := []struct {
		materialized map[string]string
		name         string
		modules      []lock.Module
		deps         []DependencyDefinition
	}{
		{
			name:         "missing digest",
			modules:      []lock.Module{{Name: "acme/app", Version: "1.2.3"}},
			deps:         []DependencyDefinition{{Component: "acme/app", Version: "1.2.3"}},
			materialized: map[string]string{"acme/app": "1.2.3"},
		},
		{
			name:         "materialized version drift",
			modules:      []lock.Module{{Name: "acme/app", Version: "1.2.3", Hash: lockedResolutionDigest}},
			deps:         []DependencyDefinition{{Component: "acme/app", Version: "1.2.3"}},
			materialized: map[string]string{"acme/app": "1.2.4"},
		},
		{
			name:         "root constraint drift",
			modules:      []lock.Module{{Name: "acme/app", Version: "1.2.3", Hash: lockedResolutionDigest}},
			deps:         []DependencyDefinition{{Component: "acme/app", Version: ">=2.0.0"}},
			materialized: map[string]string{"acme/app": "1.2.3"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := lockedResolutionHandler(t, test.modules)
			_, ok := handler.lockedResolution(test.deps, test.materialized)
			require.False(t, ok)
		})
	}
}

func TestLockedResolutionRejectsMutableReplacements(t *testing.T) {
	handler := lockedResolutionHandler(t, []lock.Module{
		{Name: "acme/app", Version: "1.2.3", Hash: lockedResolutionDigest},
	})
	handler.replacements["acme/app"] = lock.Replacement{From: "acme/app", To: "../app"}

	_, ok := handler.lockedResolution(
		[]DependencyDefinition{{Component: "acme/app", Version: "1.2.3"}},
		map[string]string{"acme/app": "1.2.3"},
	)
	require.False(t, ok)
}

func TestWorkspaceLockedVersionPolicies(t *testing.T) {
	handler := lockedResolutionHandler(t, []lock.Module{
		{Name: "acme/a", Version: "1.0.0"},
		{Name: "acme/b", Version: "2.0.0"},
		{Name: "local/selected", Version: "3.0.0"},
	})
	handler.replacements["local/selected"] = lock.Replacement{From: "local/selected", To: t.TempDir()}
	handler.replacements["local/new"] = lock.Replacement{From: "local/new", To: t.TempDir()}

	require.Equal(t, map[string]string{
		"acme/a": "1.0.0", "acme/b": "2.0.0",
		"local/selected": "3.0.0", "local/new": replacementZeroVersion,
	}, handler.workspaceLockedVersions(nil, false), "startup preserves the complete selected graph")
	require.Equal(t, map[string]string{
		"local/selected": "3.0.0", "local/new": replacementZeroVersion,
	}, handler.workspaceLockedVersions(nil, true), "full update releases only remote selections")
	require.Equal(t, map[string]string{
		"acme/b": "2.0.0", "local/selected": "3.0.0", "local/new": replacementZeroVersion,
	}, handler.workspaceLockedVersions(map[string]struct{}{"acme/a": {}}, false),
		"targeted update releases its target and preserves other selections")
}

func TestVerifiedOfflineResolutionNeverFallsBackToHub(t *testing.T) {
	handler := lockedResolutionHandler(t, []lock.Module{
		{Name: "acme/app", Version: "1.2.3"}, // Missing digest: not verified.
	})
	handler.hub = &fakeHub{
		getManifest: func(context.Context, string, string, string) (*ModuleManifest, error) {
			t.Fatal("verified-offline resolution must not call GetManifest")
			return nil, nil
		},
		listVersions: func(context.Context, string, string) ([]VersionInfo, error) {
			t.Fatal("verified-offline resolution must not call ListAllVersions")
			return nil, nil
		},
	}

	ctx := regapi.WithDependencyAccess(context.Background(), regapi.DependencyAccessVerifiedOffline)
	_, err := handler.resolveEffectiveModules(
		ctx,
		[]DependencyDefinition{{Component: "acme/app", Version: "1.2.3"}},
		map[string]string{"acme/app": "1.2.3"},
		nil,
	)
	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, apierror.Invalid, apiErr.Kind())
}

func TestVerifiedOfflineArtifactMissNeverDownloads(t *testing.T) {
	handler := &DependencyHandler{
		hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				t.Fatal("verified-offline artifact load must not call GetDownloadURL")
				return nil, nil
			},
			downloadFile: func(context.Context, string, string) error {
				t.Fatal("verified-offline artifact load must not download")
				return nil
			},
		},
		logger:       zap.NewNop(),
		vendorDir:    t.TempDir(),
		replacements: make(map[string]lock.Replacement),
	}
	ctx := regapi.WithDependencyAccess(context.Background(), regapi.DependencyAccessVerifiedOffline)
	_, err := handler.ensureModuleAvailable(ctx, ResolvedModule{
		Org: "acme", Name: "app", Version: "1.2.3",
		Digest: "sha256:" + lockedResolutionDigest,
	})
	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, apierror.Invalid, apiErr.Kind())
}

func TestVerifiedOfflineResolverUsesInstalledModuleGraph(t *testing.T) {
	vendorDir := t.TempDir()
	artifacts := map[string][]byte{
		"app": buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.app", "lib"), Kind: regapi.NamespaceDependency,
			Data: map[string]any{"component": "acme/lib", "version": "v1.0.0"},
		}}),
		"lib": buildWappBytes(t, []wapp.Entry{{ID: wapp.NewID("acme.lib", "service"), Kind: "service"}}),
	}
	modules := make([]lock.Module, 0, len(artifacts))
	for _, name := range []string{"app", "lib"} {
		sum := sha256.Sum256(artifacts[name])
		digest := "sha256:" + hex.EncodeToString(sum[:])
		modules = append(modules, lock.Module{Name: "acme/" + name, Version: "v1.0.0", Hash: digest})
		path := filepath.Join(vendorDir, "acme", name+"-v1.0.0.wapp")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, artifacts[name], 0o600))
	}
	handler := lockedResolutionHandler(t, modules)
	handler.vendorDir = vendorDir
	handler.replacements["unused/local"] = lock.Replacement{From: "unused/local", To: t.TempDir()}
	handler.hub = &fakeHub{
		getManifest: func(context.Context, string, string, string) (*ModuleManifest, error) {
			t.Fatal("installed offline resolution must not query Hub manifests")
			return nil, nil
		},
		listVersions: func(context.Context, string, string) ([]VersionInfo, error) {
			t.Fatal("installed offline resolution must not query Hub versions")
			return nil, nil
		},
	}

	ctx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
	resolved, err := handler.resolveEffectiveModules(ctx,
		[]DependencyDefinition{{Component: "acme/app", Version: "v1.0.0"}},
		map[string]string{"acme/app": "v1.0.0", "acme/lib": "v1.0.0"},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, resolved, 2)
}
