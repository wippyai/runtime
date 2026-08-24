// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	regexp "github.com/wippyai/runtime/system/registry/expansion"
	"github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

const (
	offlineReplacedModule = "local/mod"
	offlineLockedModule   = "acme/lib"
	offlineLockedVersion  = "1.0.0"
)

// offlineProviderFixture builds the verified-offline manifest provider over a
// lock that pins one materialized module and a workspace that replaces another.
func offlineProviderFixture(t *testing.T, replaced ...string) (ManifestProvider, *fakeManifestProvider) {
	t.Helper()

	digest := "sha256:" + lockedResolutionDigest
	handler := lockedResolutionHandler(t, []lock.Module{
		{Name: offlineLockedModule, Version: offlineLockedVersion, Hash: digest},
	})
	for _, module := range replaced {
		handler.replacements[module] = lock.Replacement{From: module, To: t.TempDir()}
	}

	base := newFakeProvider()
	base.addModule("local", "mod", "0.2.0")
	base.addModule("acme", "lib", "9.9.9")

	provider := newLockedManifestProvider(
		handler,
		map[string]string{offlineLockedModule: offlineLockedVersion},
		map[string]string{offlineLockedModule + "@" + offlineLockedVersion: digest},
		base,
	)
	return provider, base
}

func TestLockedManifestProviderResolvesReplacedModuleWithoutDigestEvidence(t *testing.T) {
	ctx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
	provider, base := offlineProviderFixture(t, offlineReplacedModule)

	versions, err := provider.ListAllVersions(ctx, "local", "mod")
	require.NoError(t, err)
	require.Equal(t, []VersionInfo{{Version: "0.2.0"}}, versions)

	manifest, err := provider.GetManifest(ctx, "local", "mod", "0.2.0")
	require.NoError(t, err)
	require.Equal(t, "0.2.0", manifest.Version)

	assert.Equal(t, 1, base.listAllVersion[offlineReplacedModule])
	assert.Equal(t, 1, base.getManifest[offlineReplacedModule])
}

func TestLockedManifestProviderKeepsFastPathForVerifiedLockedModule(t *testing.T) {
	ctx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
	provider, base := offlineProviderFixture(t, offlineReplacedModule)

	versions, err := provider.ListAllVersions(ctx, "acme", "lib")
	require.NoError(t, err)
	require.Equal(t, []VersionInfo{{Version: offlineLockedVersion}}, versions)
	assert.Zero(t, base.listAllVersion[offlineLockedModule], "verified lock evidence must answer locally")
}

func TestLockedManifestProviderRefusesModuleNeitherLockedNorReplaced(t *testing.T) {
	ctx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
	provider, base := offlineProviderFixture(t, offlineReplacedModule)

	_, listErr := provider.ListAllVersions(ctx, "acme", "unknown")
	requireOfflineEvidenceError(t, listErr)

	_, manifestErr := provider.GetManifest(ctx, "acme", "unknown", "1.0.0")
	requireOfflineEvidenceError(t, manifestErr)

	assert.Zero(t, base.listAllVersion["acme/unknown"], "an unverified module must not reach the network")
	assert.Zero(t, base.getManifest["acme/unknown"])
}

func TestLockedManifestProviderPrefersReplacementOverLockEntry(t *testing.T) {
	ctx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
	provider, base := offlineProviderFixture(t, offlineLockedModule)

	versions, err := provider.ListAllVersions(ctx, "acme", "lib")
	require.NoError(t, err)
	require.Equal(t, []VersionInfo{{Version: "9.9.9"}}, versions, "the replacement owns the module, not the lock entry")
	assert.Equal(t, 1, base.listAllVersion[offlineLockedModule])
}

func TestLockedManifestProviderWithoutReplacementsNeverLeavesTheLock(t *testing.T) {
	ctx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
	provider, base := offlineProviderFixture(t)

	_, err := provider.ListAllVersions(ctx, "local", "mod")
	requireOfflineEvidenceError(t, err)

	versions, err := provider.ListAllVersions(ctx, "acme", "lib")
	require.NoError(t, err)
	require.Equal(t, []VersionInfo{{Version: offlineLockedVersion}}, versions)

	assert.Zero(t, base.listAllVersion[offlineReplacedModule])
	assert.Zero(t, base.listAllVersion[offlineLockedModule])
}

func requireOfflineEvidenceError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, apierror.Invalid, apiErr.Kind())
	require.Contains(t, apiErr.Error(), "verified dependency evidence is unavailable during offline startup")
}

// offlineStartupWorkspace writes a workspace whose deployment root is a
// source-replaced module carrying a live range, plus one locked module the
// replacement depends on transitively.
type offlineStartupWorkspace struct {
	hub           *fakeHub
	registry      *registryimpl.Reg
	listCalls     map[string]int
	manifestCalls map[string]int
	lockPath      string
	vendorDir     string
	baseline      regapi.State
	downloadCalls int
}

func newOfflineStartupWorkspace(t *testing.T, replacementTarget string) *offlineStartupWorkspace {
	t.Helper()

	rootDir := t.TempDir()
	lockPath := filepath.Join(rootDir, "wippy.lock")
	vendorDir := filepath.Join(rootDir, ".wippy")
	replacementPath := filepath.Join(rootDir, "local-mod")

	artifact := buildWappBytes(t, []wapp.Entry{{
		ID: wapp.NewID("acme.lib", "service"), Kind: "service",
	}})
	sum := sha256.Sum256(artifact)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	artifactPath := filepath.Join(vendorDir, "acme", "lib-"+offlineLockedVersion+".wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(artifactPath), 0o755))
	require.NoError(t, os.WriteFile(artifactPath, artifact, 0o600))

	require.NoError(t, os.WriteFile(lockPath, []byte(`directories:
  modules: .wippy
  src: ./src
modules:
  - name: `+offlineLockedModule+`
    version: `+offlineLockedVersion+`
    hash: `+digest+`
replacements:
  - from: `+offlineReplacedModule+`
    to: `+replacementTarget+`
`), 0o600))

	require.NoError(t, os.MkdirAll(replacementPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(replacementPath, "_index.json"), []byte(`{
  "namespace": "local.mod",
  "entries": [
    {"name": "svc", "kind": "registry.entry", "data": {"generation": "one"}},
    {"name": "lib", "kind": "ns.dependency", "data": {"component": "`+offlineLockedModule+`", "version": "`+offlineLockedVersion+`"}}
  ]
}`), 0o600))

	workspace := &offlineStartupWorkspace{
		lockPath:      lockPath,
		vendorDir:     vendorDir,
		listCalls:     make(map[string]int),
		manifestCalls: make(map[string]int),
	}
	workspace.hub = &fakeHub{
		listVersions: func(_ context.Context, org, module string) ([]VersionInfo, error) {
			workspace.listCalls[org+"/"+module]++
			return []VersionInfo{{Version: "0.2.0"}}, nil
		},
		getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
			name := org + "/" + module
			workspace.manifestCalls[name]++
			if name != offlineLockedModule {
				return nil, apierror.New(apierror.NotFound, "module "+name+" is not published")
			}
			return &ModuleManifest{
				Org: org, Name: module,
				Version: offlineLockedVersion, VersionID: offlineLockedVersion,
				Digest: digest,
			}, nil
		},
		getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
			workspace.downloadCalls++
			return nil, apierror.New(apierror.NotFound, "hub downloads are not available to this workspace")
		},
	}
	workspace.baseline = regapi.State{
		{
			ID:   regapi.NewID("app.deps", "local"),
			Kind: regapi.NamespaceDependency,
			Data: payload.New(map[string]any{"component": offlineReplacedModule, "version": "*"}),
		},
		markModuleIdentity(regapi.Entry{
			ID: regapi.NewID("acme.lib", "service"), Kind: "service",
		}, offlineLockedModule, offlineLockedVersion, digest),
	}
	return workspace
}

func (w *offlineStartupWorkspace) loadState(t *testing.T, access regapi.DependencyAccess) error {
	t.Helper()

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       w.hub,
		Logger:    zap.NewNop(),
		LockPath:  w.lockPath,
		VendorDir: w.vendorDir,
	})
	require.NoError(t, err)

	resolver := topology.NewResolver()
	handler.resolver = resolver
	registry := registryimpl.NewRegistry(
		memory.New(),
		&bootRecordingRunner{},
		topology.NewStateBuilder(zap.NewNop(), resolver),
		resolver,
		zap.NewNop(),
		registryimpl.WithKindDirective(
			regapi.NamespaceDependency,
			regexp.NewDependencyDirective(handler.Expand).WithResolutionTransition(handler.ReconcileResolution),
		),
	)
	w.registry = registry

	ctx := regapi.WithDependencyAccess(newTestContext(), access)
	return registry.LoadState(ctx, w.baseline, version.New(0))
}

func TestVerifiedOfflineStartupBootsWorkspaceWithSourceReplacement(t *testing.T) {
	workspace := newOfflineStartupWorkspace(t, "./local-mod")

	require.NoError(t, workspace.loadState(t, regapi.DependencyAccessVerifiedOffline))

	entry, err := workspace.registry.GetEntry(regapi.NewID("local.mod", "svc"))
	require.NoError(t, err)
	require.Equal(t, "one", entry.Data.Data().(map[string]any)["generation"])

	_, err = workspace.registry.GetEntry(regapi.NewID("acme.lib", "service"))
	require.NoError(t, err, "a locked transitive dependency of the replacement resolves from lock evidence")

	assert.Equal(t, 1, workspace.listCalls[offlineReplacedModule], "only the replaced module's live range needs a candidate set")
	assert.Zero(t, workspace.listCalls[offlineLockedModule], "verified lock evidence answers the transitive dependency")
	assert.Empty(t, workspace.manifestCalls, "the local tree and the vendored artifact supply every manifest")
	assert.Zero(t, workspace.downloadCalls)
}

func TestOnlineStartupBootsWorkspaceWithSourceReplacement(t *testing.T) {
	workspace := newOfflineStartupWorkspace(t, "./local-mod")

	require.NoError(t, workspace.loadState(t, regapi.DependencyAccessOnline))

	_, err := workspace.registry.GetEntry(regapi.NewID("local.mod", "svc"))
	require.NoError(t, err)
	_, err = workspace.registry.GetEntry(regapi.NewID("acme.lib", "service"))
	require.NoError(t, err)
	assert.Equal(t, 1, workspace.manifestCalls[offlineLockedModule], "online startup keeps consulting the Hub for locked modules")
	assert.Zero(t, workspace.downloadCalls)
}

func TestVerifiedOfflineStartupReportsMissingReplacementPathNotMissingEvidence(t *testing.T) {
	workspace := newOfflineStartupWorkspace(t, "./absent-mod")

	lockObj, err := lock.New(workspace.lockPath)
	require.NoError(t, err)
	validateErr := lock.Validate(lockObj)
	require.Error(t, validateErr)
	require.Contains(t, validateErr.Error(), "does not exist")

	loadErr := workspace.loadState(t, regapi.DependencyAccessVerifiedOffline)
	require.Error(t, loadErr)
	require.NotContains(t, loadErr.Error(), "verified dependency evidence is unavailable during offline startup")
}
