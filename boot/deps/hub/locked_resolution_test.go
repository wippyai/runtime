// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
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
