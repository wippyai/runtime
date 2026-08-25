// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// offlineWorkspace is a runnable workspace on disk: a lock, local source trees
// the lock replaces, vendored artifacts for the modules it pins, and a Hub that
// is unreachable unless a test deliberately makes it answer. Verified-offline
// startup must never need it.
type offlineWorkspace struct {
	hub          *fakeHub
	registry     *registryimpl.Reg
	history      regapi.History
	hubCalls     map[string]int
	root         string
	lockPath     string
	lockedDigest string
	baseline     regapi.State
}

func newOfflineWorkspace(t *testing.T) *offlineWorkspace {
	t.Helper()

	workspace := &offlineWorkspace{
		root:     t.TempDir(),
		hubCalls: make(map[string]int),
	}
	workspace.lockPath = filepath.Join(workspace.root, "wippy.lock")
	workspace.hub = &fakeHub{
		listVersions: func(_ context.Context, org, module string) ([]VersionInfo, error) {
			workspace.hubCalls["list "+org+"/"+module]++
			return nil, apierror.New(apierror.Unavailable, "hub is unreachable")
		},
		getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
			workspace.hubCalls["manifest "+org+"/"+module]++
			return nil, apierror.New(apierror.Unavailable, "hub is unreachable")
		},
		getDownload: func(_ context.Context, params *DownloadParams) (*DownloadInfo, error) {
			workspace.hubCalls["download "+params.Org+"/"+params.Module]++
			return nil, apierror.New(apierror.Unavailable, "hub is unreachable")
		},
		downloadFile: func(context.Context, string, string) error {
			workspace.hubCalls["download file"]++
			return apierror.New(apierror.Unavailable, "hub is unreachable")
		},
	}
	return workspace
}

func (w *offlineWorkspace) vendorDir() string {
	return filepath.Join(w.root, ".wippy")
}

// writeSource writes a local module tree, optionally declaring its own version.
func (w *offlineWorkspace) writeSource(t *testing.T, dir, namespace, sourceVersion, entries string) {
	t.Helper()

	path := filepath.Join(w.root, dir)
	require.NoError(t, os.MkdirAll(path, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "_index.json"), []byte(
		`{"namespace": "`+namespace+`", "entries": [`+entries+`]}`,
	), 0o600))
	if sourceVersion != "" {
		require.NoError(t, os.WriteFile(filepath.Join(path, "wippy.yaml"),
			[]byte("version: "+sourceVersion+"\n"), 0o600))
	}
}

// vendorModule materializes a published module the lock can pin, and returns
// its artifact digest.
func (w *offlineWorkspace) vendorModule(t *testing.T, org, name, moduleVersion string, entries []wapp.Entry) string {
	t.Helper()

	artifact := buildWappBytes(t, entries)
	sum := sha256.Sum256(artifact)
	path := filepath.Join(w.vendorDir(), org, name+"-"+moduleVersion+".wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, artifact, 0o600))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (w *offlineWorkspace) writeLock(t *testing.T, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(w.lockPath, []byte(
		"directories:\n  modules: .wippy\n  src: ./src\n"+body,
	), 0o600))
}

func (w *offlineWorkspace) rootDependency(name, component, constraint string) regapi.Entry {
	return regapi.Entry{
		ID:             regapi.NewID("app.deps", name),
		Kind:           regapi.NamespaceDependency,
		Data:           payload.New(map[string]any{"component": component, "version": constraint}),
		DependencyRoot: true,
	}
}

// installedModule declares an entry as already materialized from a published
// module. fixtureState moves that declaration into registry-owned provenance.
func (w *offlineWorkspace) installedModule(id regapi.ID, module, moduleVersion, digest string) regapi.Entry {
	return fixtureOwned(regapi.Entry{ID: id, Kind: "service"}, module, moduleVersion, digest)
}

func (w *offlineWorkspace) newHandler(t *testing.T) *DependencyHandler {
	t.Helper()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       w.hub,
		Logger:    zap.NewNop(),
		LockPath:  w.lockPath,
		VendorDir: w.vendorDir(),
	})
	require.NoError(t, err)
	return handler
}

func (w *offlineWorkspace) newRegistry(t *testing.T, history regapi.History) *registryimpl.Reg {
	t.Helper()

	handler := w.newHandler(t)
	resolver := topology.NewResolver()
	handler.resolver = resolver
	return registryimpl.NewRegistry(
		history,
		&bootRecordingRunner{},
		topology.NewStateBuilder(zap.NewNop(), resolver),
		resolver,
		zap.NewNop(),
		registryimpl.WithKindDirective(
			regapi.NamespaceDependency,
			regexp.NewDependencyDirective(handler.Expand).WithResolutionTransition(handler.ReconcileResolution),
		),
	)
}

// loadState boots the workspace from its baseline, as a cold start does.
func (w *offlineWorkspace) loadState(t *testing.T, access regapi.DependencyAccess) error {
	t.Helper()

	if w.history == nil {
		w.history = memory.New()
	}
	w.registry = w.newRegistry(t, w.history)
	ctx := regapi.WithDependencyAccess(newTestContext(), access)
	return w.registry.LoadState(ctx, fixtureState(w.baseline), version.New(0))
}

func (w *offlineWorkspace) requireEntry(t *testing.T, namespace, name string) regapi.Entry {
	t.Helper()

	entry, err := w.registry.GetEntry(regapi.NewID(namespace, name))
	require.NoError(t, err)
	return entry
}

func (w *offlineWorkspace) requireProvenance(t *testing.T, id regapi.ID) regapi.EntryProvenance {
	t.Helper()

	_, state, err := w.registry.SnapshotState()
	require.NoError(t, err)
	record, ok := state.Provenance[id]
	require.True(t, ok, "missing provenance for %s", id.String())
	return record
}

func (w *offlineWorkspace) requireHubUntouched(t *testing.T) {
	t.Helper()
	require.Empty(t, w.hubCalls, "verified-offline startup must not reach the Hub")
}

// rootAPIError returns the innermost api error in the chain: registry boot
// wraps a directive failure, and the failure's own kind is what matters here.
func rootAPIError(t *testing.T, err error) apierror.Error {
	t.Helper()

	var found apierror.Error
	for current := err; current != nil; current = errors.Unwrap(current) {
		var apiErr apierror.Error
		if errors.As(current, &apiErr) {
			found = apiErr
		}
	}
	require.NotNil(t, found, "expected an api error in %v", err)
	return found
}

// sourceEntry renders one entry of a local module tree.
func sourceEntry(name, kind, data string) string {
	return `{"name": "` + name + `", "kind": "` + kind + `", "data": ` + data + `}`
}

func dependencyEntry(name, component, constraint string) string {
	return sourceEntry(name, regapi.NamespaceDependency,
		`{"component": "`+component+`", "version": "`+constraint+`"}`)
}

// singleReplacementWorkspace is the reported regression: a deployment root
// backed by a local source tree that declares no version of its own, requested
// through a live range, with one published module beneath it.
func singleReplacementWorkspace(t *testing.T, constraint string) *offlineWorkspace {
	t.Helper()

	workspace := newOfflineWorkspace(t)
	digest := workspace.vendorModule(t, "acme", "lib", "1.0.0", []wapp.Entry{{
		ID: wapp.NewID("acme.lib", "service"), Kind: "service",
	}})
	workspace.lockedDigest = digest
	workspace.writeSource(t, "local-mod", "local.mod", "",
		sourceEntry("svc", "registry.entry", `{"generation": "one"}`)+","+
			dependencyEntry("lib", "acme/lib", "1.0.0"),
	)
	workspace.writeLock(t, `modules:
  - name: acme/lib
    version: 1.0.0
    hash: `+digest+`
replacements:
  - from: local/mod
    to: ./local-mod
`)
	workspace.baseline = regapi.State{
		workspace.rootDependency("local", "local/mod", constraint),
		workspace.installedModule(regapi.NewID("acme.lib", "service"), "acme/lib", "1.0.0", digest),
	}
	return workspace
}

func TestVerifiedOfflineStartupResolvesReplacementWithUnreachableHub(t *testing.T) {
	workspace := singleReplacementWorkspace(t, "*")

	require.NoError(t, workspace.loadState(t, regapi.DependencyAccessVerifiedOffline))

	entry := workspace.requireEntry(t, "local.mod", "svc")
	require.Equal(t, "one", entry.Data.Data().(map[string]any)["generation"])
	workspace.requireEntry(t, "acme.lib", "service")
	workspace.requireHubUntouched(t)
}

func TestVerifiedOfflineStartupResolvesReplacementUnderLabelConstraint(t *testing.T) {
	workspace := singleReplacementWorkspace(t, "@latest")

	require.NoError(t, workspace.loadState(t, regapi.DependencyAccessVerifiedOffline))

	workspace.requireEntry(t, "local.mod", "svc")
	workspace.requireHubUntouched(t)
}

func TestVerifiedOfflineStartupFailsWhenRangeOutrunsLocalEvidence(t *testing.T) {
	workspace := newOfflineWorkspace(t)
	digest := workspace.vendorModule(t, "local", "mod", "1.0.0", []wapp.Entry{{
		ID: wapp.NewID("local.mod", "svc"), Kind: "registry.entry",
		Data: map[string]any{"generation": "published"},
	}})
	workspace.writeSource(t, "local-mod", "local.mod", "",
		sourceEntry("svc", "registry.entry", `{"generation": "local"}`))
	workspace.writeLock(t, `modules:
  - name: local/mod
    version: 1.0.0
    hash: `+digest+`
replacements:
  - from: local/mod
    to: ./local-mod
`)
	workspace.baseline = regapi.State{
		workspace.rootDependency("local", "local/mod", ">=9.0.0"),
		workspace.installedModule(regapi.NewID("local.mod", "other"), "local/mod", "1.0.0", digest),
	}

	err := workspace.loadState(t, regapi.DependencyAccessVerifiedOffline)

	// Nothing local shows the tree is >=9.0.0, and a durable resolution may
	// not select a version its own declaration excludes. Saying so beats both
	// reaching for the Hub and binding the tree to a release on no evidence.
	require.Error(t, err)
	require.Contains(t, err.Error(), "no available version of local/mod")
	require.NotContains(t, err.Error(), "verified dependency evidence is unavailable during offline startup")
	workspace.requireHubUntouched(t)
}

func TestVerifiedOfflineStartupUsesDeclaredSourceVersionForRange(t *testing.T) {
	workspace := newOfflineWorkspace(t)
	workspace.writeSource(t, "local-mod", "local.mod", "9.1.0",
		sourceEntry("svc", "registry.entry", `{"generation": "local"}`))
	workspace.writeLock(t, `replacements:
  - from: local/mod
    to: ./local-mod
`)
	workspace.baseline = regapi.State{workspace.rootDependency("local", "local/mod", ">=9.0.0")}

	require.NoError(t, workspace.loadState(t, regapi.DependencyAccessVerifiedOffline))

	entry := workspace.requireEntry(t, "local.mod", "svc")
	require.Equal(t, "local", entry.Data.Data().(map[string]any)["generation"])
	require.Equal(t, "9.1.0", workspace.requireProvenance(t, entry.ID).Version,
		"the version the source declares settles a live range offline")
	workspace.requireHubUntouched(t)
}

func TestVerifiedOfflineStartupKeepsRecordedLabelForReplacedLockedModule(t *testing.T) {
	workspace := newOfflineWorkspace(t)
	digest := workspace.vendorModule(t, "local", "mod", "1.0.0", []wapp.Entry{{
		ID: wapp.NewID("local.mod", "svc"), Kind: "registry.entry",
		Data: map[string]any{"generation": "published"},
	}})
	workspace.writeSource(t, "local-mod", "local.mod", "",
		sourceEntry("svc", "registry.entry", `{"generation": "local"}`))
	workspace.writeLock(t, `modules:
  - name: local/mod
    version: 1.0.0
    hash: `+digest+`
replacements:
  - from: local/mod
    to: ./local-mod
`)
	workspace.baseline = regapi.State{
		workspace.rootDependency("local", "local/mod", ">=1.0.0"),
		workspace.installedModule(regapi.NewID("local.mod", "other"), "local/mod", "1.0.0", digest),
	}

	require.NoError(t, workspace.loadState(t, regapi.DependencyAccessVerifiedOffline))

	entry := workspace.requireEntry(t, "local.mod", "svc")
	require.Equal(t, "local", entry.Data.Data().(map[string]any)["generation"],
		"the replacement supplies the content of a module the lock also pins")
	require.Equal(t, "1.0.0", workspace.requireProvenance(t, entry.ID).Version,
		"the recorded label stays with it")
	workspace.requireHubUntouched(t)
}

func TestVerifiedOfflineStartupResolvesTransitivelyReplacedDependency(t *testing.T) {
	workspace := newOfflineWorkspace(t)
	workspace.writeSource(t, "local-app", "local.app", "",
		sourceEntry("svc", "registry.entry", `{"generation": "app"}`)+","+
			dependencyEntry("lib", "local/lib", ">=2.0.0"),
	)
	workspace.writeSource(t, "local-lib", "local.lib", "2.5.0",
		sourceEntry("svc", "registry.entry", `{"generation": "lib"}`))
	workspace.writeLock(t, `replacements:
  - from: local/app
    to: ./local-app
  - from: local/lib
    to: ./local-lib
`)
	workspace.baseline = regapi.State{workspace.rootDependency("app", "local/app", "*")}

	require.NoError(t, workspace.loadState(t, regapi.DependencyAccessVerifiedOffline))

	workspace.requireEntry(t, "local.app", "svc")
	entry := workspace.requireEntry(t, "local.lib", "svc")
	require.Equal(t, "2.5.0", workspace.requireProvenance(t, entry.ID).Version,
		"a replaced module's own source version labels it")
	workspace.requireHubUntouched(t)
}

// failingReplacementWorkspace pairs a replaced module whose local tree is
// invalid with a module that has no offline evidence at all. The names decide
// which failure the resolver reports first.
func failingReplacementWorkspace(t *testing.T, replacedOrg, unverifiedOrg string) *offlineWorkspace {
	t.Helper()

	workspace := newOfflineWorkspace(t)
	workspace.writeSource(t, "local-mod", replacedOrg+".mod", "",
		dependencyEntry("broken", "", "1.0.0"))
	workspace.writeLock(t, `replacements:
  - from: `+replacedOrg+`/mod
    to: ./local-mod
`)
	workspace.baseline = regapi.State{
		workspace.rootDependency("replaced", replacedOrg+"/mod", "*"),
		workspace.rootDependency("unverified", unverifiedOrg+"/lib", ">=1.0.0"),
	}
	return workspace
}

func TestVerifiedOfflineStartupReportsReplacementFailureWhateverTheModuleOrder(t *testing.T) {
	orders := []struct {
		name          string
		replacedOrg   string
		unverifiedOrg string
	}{
		{name: "unverified module sorts first", replacedOrg: "zulu", unverifiedOrg: "alpha"},
		{name: "replaced module sorts first", replacedOrg: "alpha", unverifiedOrg: "zulu"},
	}

	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			workspace := failingReplacementWorkspace(t, order.replacedOrg, order.unverifiedOrg)

			err := workspace.loadState(t, regapi.DependencyAccessVerifiedOffline)

			require.Error(t, err)
			apiErr := rootAPIError(t, err)
			require.Equal(t, apierror.Conflict, apiErr.Kind(),
				"a replaced module's failure is never a missing-evidence failure")
			require.Contains(t, apiErr.Error(), order.replacedOrg+"/mod@*: invalid dependency entry",
				"the replacement's real failure stays visible")
			require.Contains(t, apiErr.Error(), order.unverifiedOrg+"/lib",
				"the module without evidence is still reported")
		})
	}
}

func TestVerifiedOfflineStartupKeepsEvidenceRefusalWithoutReplacements(t *testing.T) {
	workspace := newOfflineWorkspace(t)
	workspace.writeLock(t, "")
	workspace.baseline = regapi.State{workspace.rootDependency("unverified", "acme/lib", ">=1.0.0")}

	err := workspace.loadState(t, regapi.DependencyAccessVerifiedOffline)

	require.Error(t, err)
	apiErr := rootAPIError(t, err)
	require.Equal(t, apierror.Invalid, apiErr.Kind())
	require.Contains(t, apiErr.Error(), "verified dependency evidence is unavailable during offline startup")
	require.Empty(t, workspace.hubCalls, "an unverified module must not fall back to the Hub")
}

func TestVerifiedOfflineStartupReportsMissingReplacementPath(t *testing.T) {
	workspace := singleReplacementWorkspace(t, "*")
	require.NoError(t, os.RemoveAll(filepath.Join(workspace.root, "local-mod")))

	err := workspace.loadState(t, regapi.DependencyAccessVerifiedOffline)

	require.Error(t, err)
	require.NotContains(t, err.Error(), "verified dependency evidence is unavailable during offline startup")
	require.Contains(t, err.Error(), "local-mod", "the unreadable replacement tree is named")
	workspace.requireHubUntouched(t)
}

func TestVerifiedOfflineStartupResolvesFromLockAfterReplacementRemoved(t *testing.T) {
	workspace := newOfflineWorkspace(t)
	digest := workspace.vendorModule(t, "local", "mod", "1.0.0", []wapp.Entry{{
		ID: wapp.NewID("local.mod", "svc"), Kind: "registry.entry",
		Data: map[string]any{"generation": "published"},
	}})
	workspace.writeSource(t, "local-mod", "local.mod", "",
		sourceEntry("svc", "registry.entry", `{"generation": "local"}`))
	lockedModules := `modules:
  - name: local/mod
    version: 1.0.0
    hash: ` + digest + `
`
	workspace.writeLock(t, lockedModules+`replacements:
  - from: local/mod
    to: ./local-mod
`)
	workspace.baseline = regapi.State{
		workspace.rootDependency("local", "local/mod", "1.0.0"),
		workspace.installedModule(regapi.NewID("local.mod", "other"), "local/mod", "1.0.0", digest),
	}

	historyPath := filepath.Join(workspace.root, "registry.db")
	history, err := historysqlite.NewSQLite(historyPath, zap.NewNop())
	require.NoError(t, err)
	workspace.history = history
	require.NoError(t, workspace.loadState(t, regapi.DependencyAccessVerifiedOffline))
	replaced := workspace.requireEntry(t, "local.mod", "svc")
	require.Equal(t, "local", replaced.Data.Data().(map[string]any)["generation"])
	require.Contains(t, workspace.requireProvenance(t, replaced.ID).Digest, "sha256-tree-v1:",
		"the tree's own identity pins a replaced module")
	require.NoError(t, history.Close())

	// The operator drops the replacement; the lock alone must carry the boot.
	workspace.writeLock(t, lockedModules)
	history, err = historysqlite.NewSQLite(historyPath, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = history.Close() })
	workspace.history = history

	require.NoError(t, workspace.loadState(t, regapi.DependencyAccessVerifiedOffline))
	_, state, err := workspace.registry.SnapshotState()
	require.NoError(t, err)
	owned := 0
	for _, entry := range state.Entries {
		record := state.Provenance[entry.ID]
		if record.Module != "local/mod" {
			continue
		}
		owned++
		require.Equal(t, digest, record.Digest,
			"the lock's artifact backs the module once the replacement is gone")
	}
	require.NotZero(t, owned, "the locked module remains resident")
	workspace.requireHubUntouched(t)
}

func TestOnlineStartupResolvesReplacementThroughHub(t *testing.T) {
	workspace := singleReplacementWorkspace(t, "*")
	workspace.hub.listVersions = func(_ context.Context, org, module string) ([]VersionInfo, error) {
		workspace.hubCalls["list "+org+"/"+module]++
		return []VersionInfo{{Version: "0.2.0"}}, nil
	}
	workspace.hub.getManifest = func(_ context.Context, org, module, constraint string) (*ModuleManifest, error) {
		workspace.hubCalls["manifest "+org+"/"+module]++
		return &ModuleManifest{
			Org: org, Name: module, Version: constraint, VersionID: constraint,
			Digest: workspace.lockedDigest,
		}, nil
	}

	require.NoError(t, workspace.loadState(t, regapi.DependencyAccessOnline))

	entry := workspace.requireEntry(t, "local.mod", "svc")
	require.Equal(t, "0.2.0", workspace.requireProvenance(t, entry.ID).Version,
		"online resolution still lets the Hub label an unversioned replacement")
	require.Equal(t, 1, workspace.hubCalls["list local/mod"])
}

// offlineReplacementProvider builds the production provider stack for a
// replacement whose tree declares sourceVersion, over a base that fails if it
// is consulted at all.
func offlineReplacementProvider(t *testing.T, sourceVersion, recorded string) (*replacementManifestProvider, *fakeManifestProvider) {
	t.Helper()

	path := t.TempDir()
	if sourceVersion != "" {
		require.NoError(t, os.WriteFile(filepath.Join(path, "wippy.yaml"),
			[]byte("version: "+sourceVersion+"\n"), 0o600))
	}
	base := newFakeProvider()
	base.addModule("local", "mod", "7.7.7")

	return &replacementManifestProvider{
		base: base,
		handler: &DependencyHandler{
			logger: zap.NewNop(),
			replacements: map[string]lock.Replacement{
				"local/mod": {From: "local/mod", To: path},
			},
		},
		lockedVersions: map[string]string{"local/mod": recorded},
	}, base
}

func TestReplacementProviderLabelsFromLocalEvidenceOffline(t *testing.T) {
	tests := []struct {
		name          string
		sourceVersion string
		recorded      string
		want          string
	}{
		{name: "declared source version", sourceVersion: "2.5.0", recorded: "1.0.0", want: "2.5.0"},
		{name: "recorded version", recorded: "1.0.0", want: "1.0.0"},
		{name: "zero release when nothing names one", want: replacementZeroVersion},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
			provider, base := offlineReplacementProvider(t, test.sourceVersion, test.recorded)

			versions, err := provider.ListAllVersions(ctx, "local", "mod")
			require.NoError(t, err)
			require.Equal(t, []VersionInfo{{Version: test.want}}, versions)

			manifest, err := provider.GetManifest(ctx, "local", "mod", "@latest")
			require.NoError(t, err)
			require.Equal(t, test.want, manifest.Version)

			assert.Zero(t, base.listAllVersion["local/mod"], "offline startup must not enumerate releases")
			assert.Zero(t, base.getManifest["local/mod"], "offline startup must not resolve labels remotely")
		})
	}
}

func TestReplacementProviderStillDelegatesLabelsOnline(t *testing.T) {
	provider, base := offlineReplacementProvider(t, "", "")

	versions, err := provider.ListAllVersions(newTestContext(), "local", "mod")
	require.NoError(t, err)
	require.Equal(t, []VersionInfo{{Version: "7.7.7"}}, versions)
	assert.Equal(t, 1, base.listAllVersion["local/mod"])
}

func TestReplacementProviderLeavesUnreplacedModulesToTheBaseOffline(t *testing.T) {
	ctx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
	provider, base := offlineReplacementProvider(t, "", "")
	base.addModule("acme", "lib", "1.0.0")

	versions, err := provider.ListAllVersions(ctx, "acme", "lib")
	require.NoError(t, err)
	require.Equal(t, []VersionInfo{{Version: "1.0.0"}}, versions)
	assert.Equal(t, 1, base.listAllVersion["acme/lib"],
		"only replaced modules resolve from local evidence")
}
