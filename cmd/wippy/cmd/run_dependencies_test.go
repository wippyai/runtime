// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
)

func TestLockSatisfiesSource(t *testing.T) {
	lockObj, err := lock.New(filepath.Join(t.TempDir(), "wippy.lock"))
	require.NoError(t, err)
	lockObj.SetModule(lock.Module{Name: "acme/core", Version: "1.4.0", Hash: "sha256:core"})
	lockObj.SetReplacement(lock.Replacement{From: "acme/local", To: t.TempDir()})

	require.True(t, lockSatisfiesSource(lockObj, []dependencyRequest{
		{Org: "acme", Module: "core", Constraint: "^1.2.0"},
		{Org: "acme", Module: "local", Constraint: "*"},
	}))
	require.False(t, lockSatisfiesSource(lockObj, []dependencyRequest{
		{Org: "acme", Module: "views", Constraint: ">=2.0.0"},
	}))
	require.False(t, lockSatisfiesSource(lockObj, []dependencyRequest{
		{Org: "acme", Module: "core", Constraint: ">=2.0.0"},
	}))

	lockObj.SetModule(lock.Module{Name: "acme/core", Version: "1.4.0"})
	require.False(t, lockSatisfiesSource(lockObj, []dependencyRequest{
		{Org: "acme", Module: "core", Constraint: "^1.2.0"},
	}))
}

func TestResolveRunDependenciesKeepsCompatibleLockAndCompletesIt(t *testing.T) {
	// Existing locks and the current Hub both carry the bare SHA-256 form.
	// Keeping it exact here prevents one-sided normalization from rejecting
	// identical artifacts during lock completion.
	const coreDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const viewsDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	provider := runManifestProvider{manifests: map[string]hub.ModuleManifest{
		"acme/core@1.4.0": {
			Org: "acme", Name: "core", Version: "1.4.0", Digest: coreDigest,
			Dependencies: []hub.ManifestDep{{Org: "acme", Name: "views", Constraint: "^2.0.0"}},
		},
		"acme/views@2.1.0": {
			Org: "acme", Name: "views", Version: "2.1.0", Digest: viewsDigest,
		},
	}, versions: map[string][]hub.VersionInfo{
		"acme/core":  {{Version: "1.5.0"}, {Version: "1.4.0"}},
		"acme/views": {{Version: "2.1.0"}},
	}}

	lockObj, err := lock.New(filepath.Join(t.TempDir(), "wippy.lock"))
	require.NoError(t, err)
	lockObj.SetModule(lock.Module{Name: "acme/core", Version: "1.4.0", Hash: coreDigest})
	require.NoError(t, lockObj.Write())

	result, err := resolveRunDependencies(context.Background(), provider, lockObj, []dependencyRequest{
		{Org: "acme", Module: "core", Constraint: "^1.0.0"},
	})
	require.NoError(t, err)
	require.Equal(t, []hub.ResolvedModule{
		{Org: "acme", Name: "core", Version: "1.4.0", Digest: coreDigest},
		{Org: "acme", Name: "views", Version: "2.1.0", Digest: viewsDigest},
	}, result.Modules)
}

func TestResolveRunDependenciesSelectsVersionForUnversionedReplacement(t *testing.T) {
	ctx := setupLoaderContext(t)
	replacement := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(replacement, "_index.yaml"), []byte(`version: "1.0"
namespace: acme.local
entries: []
`), 0o600))

	lockObj, err := lock.New(filepath.Join(t.TempDir(), "wippy.lock"))
	require.NoError(t, err)
	lockObj.SetModule(lock.Module{Name: "acme/local", Version: "1.0.0"})
	lockObj.SetReplacement(lock.Replacement{From: "acme/local", To: replacement})
	require.NoError(t, lockObj.Write())

	provider := runManifestProvider{versions: map[string][]hub.VersionInfo{
		"acme/local": {{Version: "1.2.0"}, {Version: "1.0.0"}},
	}}
	result, err := resolveRunDependencies(ctx, provider, lockObj, []dependencyRequest{
		{Org: "acme", Module: "local", Constraint: ">=1.2.0"},
	})
	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	require.Equal(t, "1.2.0", result.Modules[0].Version)
}

type runManifestProvider struct {
	manifests map[string]hub.ModuleManifest
	versions  map[string][]hub.VersionInfo
}

func (p runManifestProvider) GetManifest(_ context.Context, org, module, constraint string) (*hub.ModuleManifest, error) {
	manifest, ok := p.manifests[org+"/"+module+"@"+constraint]
	if !ok {
		return nil, fmt.Errorf("manifest not found")
	}
	copy := manifest
	return &copy, nil
}

func (p runManifestProvider) ListAllVersions(_ context.Context, org, module string) ([]hub.VersionInfo, error) {
	versions, ok := p.versions[org+"/"+module]
	if !ok {
		return nil, fmt.Errorf("versions not found")
	}
	return append([]hub.VersionInfo(nil), versions...), nil
}

func (p runManifestProvider) GetDownloadURL(context.Context, *hub.DownloadParams) (*hub.DownloadInfo, error) {
	return nil, fmt.Errorf("download not expected")
}

func (p runManifestProvider) DownloadToFile(context.Context, string, string) error {
	return fmt.Errorf("download not expected")
}
