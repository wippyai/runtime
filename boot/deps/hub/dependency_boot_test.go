// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/runtime/internal/version"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	registryimpl "github.com/wippyai/runtime/system/registry"
	regexp "github.com/wippyai/runtime/system/registry/expansion"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type bootRecordingRunner struct {
	transitions []regapi.ChangeSet
}

func TestDependencyHandler_DeploymentRootSelfUpdateRepairsStoredResolution(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "wippy.lock")
	vendorDir := filepath.Join(tmpDir, "vendor")
	dbPath := filepath.Join(tmpDir, "registry.db")

	artifacts := map[string][]byte{
		"v1.0.0": buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.app", "service"), Kind: "service", Data: map[string]any{"version": "v1"},
		}}),
		"v2.0.0": buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.app", "service"), Kind: "service", Data: map[string]any{"version": "v2"},
		}}),
	}
	digests := make(map[string]string, len(artifacts))
	for selected, artifact := range artifacts {
		sum := sha256.Sum256(artifact)
		digests[selected] = "sha256:" + hex.EncodeToString(sum[:])
	}
	writeLock := func(selected string) {
		require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`directories:
  modules: vendor
modules:
  - name: acme/app
    version: %s
    hash: %s
    root: true
`, selected, digests[selected])), 0o600))
	}
	baseline := func(selected string) regapi.ProvenancedState {
		root := regapi.Entry{
			ID: regapi.NewID("app.deps", "app"), Kind: regapi.NamespaceDependency,
			DependencyRoot: true,
			Data:           payload.New(map[string]any{"component": "acme/app", "version": selected}),
		}
		service := regapi.Entry{
			ID: regapi.NewID("acme.app", "service"), Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{
				fixtureModuleKey:        "acme/app",
				fixtureModuleVersionKey: selected,
				fixtureModuleDigestKey:  digests[selected],
			}),
			Data: payload.New(map[string]any{"version": strings.TrimPrefix(selected, "v")}),
		}
		return fixtureState(regapi.State{root, service})
	}
	newHub := func(selected string, calls *int) HubClient {
		return &fakeHub{
			getManifest: func(_ context.Context, org, module, constraint string) (*ModuleManifest, error) {
				*calls++
				if org != "acme" || module != "app" || constraint != selected {
					return nil, fmt.Errorf("unexpected manifest request %s/%s@%s", org, module, constraint)
				}
				return &ModuleManifest{
					Org: org, Name: module, Version: selected, VersionID: selected,
					Digest: digests[selected], SizeBytes: uint64(len(artifacts[selected])), URL: "memory://" + selected,
				}, nil
			},
			downloadFile: func(_ context.Context, _ string, destination string) error {
				if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
					return err
				}
				return os.WriteFile(destination, artifacts[selected], 0o600)
			},
		}
	}
	newRegistry := func(history regapi.History, client HubClient, logger *zap.Logger, prov *registryProvenance) *registryimpl.Reg {
		resolver := topology.NewResolver()
		handler, err := NewDependencyHandler(DependencyHandlerOptions{
			Hub: client, Logger: logger, Resolver: resolver, LockPath: lockPath, VendorDir: vendorDir,
		})
		require.NoError(t, err)
		return registryimpl.NewRegistry(
			history, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop(),
			registryimpl.WithKindDirective(regapi.NamespaceDependency,
				regexp.NewDependencyDirective(prov.expand(handler)).WithResolutionTransition(prov.reconcile(handler))),
		)
	}

	writeLock("v1.0.0")
	history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	v1Calls := 0
	initialProv := newRegistryProvenance(baseline("v1.0.0"))
	initial := newRegistry(history, newHub("v1.0.0", &v1Calls), zap.NewNop(), initialProv)
	require.NoError(t, initial.LoadState(ctx, baseline("v1.0.0").Entries, version.FromParent(nil, 0)))
	_, err = initial.Apply(ctx, regapi.ChangeSet{{
		Kind:  regapi.EntryCreate,
		Entry: regapi.Entry{ID: regapi.NewID("user.settings", "theme"), Kind: regapi.EntryKind, Data: payload.New("dark")},
	}})
	require.NoError(t, err)
	head, err := history.Head()
	require.NoError(t, err)
	oldResolution, err := history.GetDependencyResolution(head)
	require.NoError(t, err)
	require.NoError(t, history.Close())

	writeLock("v2.0.0")
	history, err = historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	failing := newRegistry(history, &fakeHub{getManifest: func(context.Context, string, string, string) (*ModuleManifest, error) {
		return nil, errors.New("injected resolution failure")
	}}, zap.NewNop(), newRegistryProvenance(baseline("v2.0.0")))
	require.Error(t, failing.LoadState(ctx, baseline("v2.0.0").Entries, head))
	unchanged, err := history.GetDependencyResolution(head)
	require.NoError(t, err)
	require.Equal(t, oldResolution.Digest, unchanged.Digest, "a failed transition must not rebind history")
	require.NoError(t, history.Close())

	history, err = historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	core, logs := observer.New(zap.WarnLevel)
	v2Calls := 0
	updatedProv := newRegistryProvenance(baseline("v2.0.0"))
	updated := newRegistry(history, newHub("v2.0.0", &v2Calls), zap.New(core), updatedProv)
	require.NoError(t, updated.LoadState(ctx, baseline("v2.0.0").Entries, head))
	require.Equal(t, 1, v2Calls, "a changed deployment baseline must resolve the final root graph once")
	service, err := updated.GetEntry(regapi.NewID("acme.app", "service"))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", updatedProv.residentVersion(service.ID))
	_, err = updated.GetEntry(regapi.NewID("user.settings", "theme"))
	require.NoError(t, err, "history-owned overlays must survive a root package update")
	entries := logs.FilterMessage("stored dependency resolution does not match deployment baseline; resolving final declarations").All()
	require.Len(t, entries, 1)
	require.Equal(t, "deployment baseline changed", entries[0].ContextMap()["reason"])
	repaired, err := history.GetDependencyResolution(head)
	require.NoError(t, err)
	require.NotEmpty(t, repaired.BaselineDigest)
	require.Equal(t, "v2.0.0", repaired.Modules[0].Version)
	require.NoError(t, history.Close())

	// Once repaired, the exact graph is restart-safe. Undo rebinds the older
	// declarative version once to the current deployment; redo then reuses its
	// already-repaired graph without another resolution.
	history, err = historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = history.Close() })
	restartCalls := 0
	restartedProv := newRegistryProvenance(baseline("v2.0.0"))
	restarted := newRegistry(history, newHub("v2.0.0", &restartCalls), zap.NewNop(), restartedProv)
	require.NoError(t, restarted.LoadState(ctx, baseline("v2.0.0").Entries, head))
	require.Zero(t, restartCalls)
	service, err = restarted.GetEntry(regapi.NewID("acme.app", "service"))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", restartedProv.residentVersion(service.ID))
	require.NoError(t, restarted.ApplyVersion(ctx, version.New(regapi.RootVersion)))
	require.Equal(t, 1, restartCalls, "the older version is rebound to the new deployment once")
	_, err = restarted.GetEntry(regapi.NewID("user.settings", "theme"))
	require.Error(t, err)
	service, err = restarted.GetEntry(regapi.NewID("acme.app", "service"))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", restartedProv.residentVersion(service.ID), "undo must never roll back the root deployment")
	require.NoError(t, restarted.ApplyVersion(ctx, head))
	require.Equal(t, 1, restartCalls, "redo must reuse the repaired target graph")
	_, err = restarted.GetEntry(regapi.NewID("user.settings", "theme"))
	require.NoError(t, err)
}

func TestDependencyHandler_PersistedResolutionBootRollbackRedoAndLongHistory(t *testing.T) {
	ctx := newTestContext()
	vendorDir := filepath.Join(t.TempDir(), "vendor")
	artifacts := map[string][]byte{
		"v1.0.0": buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.crm", "service"), Kind: "service", Data: map[string]any{"version": "v1"},
		}}),
		"v2.0.0": buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.crm", "service"), Kind: "service", Data: map[string]any{"version": "v2"},
		}}),
	}
	downloads := 0
	online := &fakeHub{
		getManifest: func(_ context.Context, org, module, constraint string) (*ModuleManifest, error) {
			artifact, ok := artifacts[constraint]
			if !ok {
				return nil, fmt.Errorf("unexpected constraint %q", constraint)
			}
			sum := sha256.Sum256(artifact)
			return &ModuleManifest{
				Org: org, Name: module, Version: constraint, VersionID: constraint,
				Digest: hex.EncodeToString(sum[:]), SizeBytes: uint64(len(artifact)), URL: "memory://" + constraint,
			}, nil
		},
		downloadFile: func(_ context.Context, url, destPath string) error {
			version := strings.TrimPrefix(url, "memory://")
			downloads++
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}
			return os.WriteFile(destPath, artifacts[version], 0o600)
		},
	}
	newHandler := func(client HubClient) *DependencyHandler {
		handler, err := NewDependencyHandler(DependencyHandlerOptions{
			Hub: client, Logger: zap.NewNop(), LockPath: filepath.Join(filepath.Dir(vendorDir), "wippy.lock"), VendorDir: vendorDir,
		})
		require.NoError(t, err)
		return handler
	}
	newRegistry := func(hist regapi.History, handler *DependencyHandler, prov *registryProvenance) *registryimpl.Reg {
		resolver := topology.NewResolver()
		handler.resolver = resolver
		return registryimpl.NewRegistry(
			hist, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop(),
			registryimpl.WithKindDirective(regapi.NamespaceDependency, regexp.NewDependencyDirective(prov.expand(handler)).WithResolutionTransition(prov.reconcile(handler))),
		)
	}

	dbPath := filepath.Join(t.TempDir(), "registry.db")
	history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	liveProv := newRegistryProvenance(regapi.ProvenancedState{})
	reg := newRegistry(history, newHandler(online), liveProv)
	v1Root := regapi.Entry{
		ID: regapi.NewID("app.deps", "crm"), Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/crm","version":"v1.0.0"}`, payload.JSON),
	}
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: v1Root}})
	require.NoError(t, err)
	require.Equal(t, 1, downloads)
	v1Resolution, err := history.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", v1Resolution.Modules[0].Version)

	// A long tail of unrelated versions inherits one content-addressed graph.
	for i := 0; i < 250; i++ {
		_, err = reg.Apply(ctx, regapi.ChangeSet{{
			Kind:  regapi.EntryCreate,
			Entry: regapi.Entry{ID: regapi.NewID("app.settings", fmt.Sprintf("entry_%03d", i)), Kind: regapi.EntryKind, Data: payload.New(i)},
		}})
		require.NoError(t, err)
	}

	v2Root := v1Root
	v2Root.Data = payload.NewPayload(`{"component":"acme/crm","version":"v2.0.0"}`, payload.JSON)
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: v2Root}})
	require.NoError(t, err)
	require.Equal(t, 2, downloads)
	require.NoError(t, history.Close())

	// Reopen the actual history database so the restore cannot accidentally
	// depend on in-memory registry or resolution state.
	history, err = historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = history.Close() })
	v2, err := history.Head()
	require.NoError(t, err)
	versions, err := history.Versions()
	require.NoError(t, err)
	require.Greater(t, len(versions), 1)
	v1 = versions[1]

	manifestCalls := 0
	offline := &fakeHub{
		getManifest: func(context.Context, string, string, string) (*ModuleManifest, error) {
			manifestCalls++
			return nil, errors.New("resolver must not run while restoring an exact graph")
		},
		getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
			return nil, errors.New("cached exact artifacts should be used")
		},
	}
	restoredProv := newRegistryProvenance(regapi.ProvenancedState{})
	restored := newRegistry(history, newHandler(offline), restoredProv)
	require.NoError(t, restored.LoadState(ctx, nil, v2))
	require.Zero(t, manifestCalls)
	entry, err := restored.GetEntry(regapi.NewID("acme.crm", "service"))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", restoredProv.residentVersion(entry.ID))

	// Undo and redo select stored graphs and never resolve them again.
	require.NoError(t, restored.ApplyVersion(ctx, v1))
	entry, err = restored.GetEntry(regapi.NewID("acme.crm", "service"))
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", restoredProv.residentVersion(entry.ID))
	require.NoError(t, restored.ApplyVersion(ctx, v2))
	entry, err = restored.GetEntry(regapi.NewID("acme.crm", "service"))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", restoredProv.residentVersion(entry.ID))
	require.Zero(t, manifestCalls)
}

func TestDependencyHandler_ApplyVersionReconciliationSemantics(t *testing.T) {
	t.Run("removes departing graph without touching deployment owner", func(t *testing.T) {
		packRegistry := embedpkg.NewRegistry()
		defer func() { require.NoError(t, packRegistry.Close()) }()
		ctx := embedapi.WithRegistry(newTestContext(), packRegistry)
		reg, runner, baselineVersion, hostID := newLegacyArtifactRollbackRegistry(ctx, t)

		addonRoot := regapi.Entry{
			ID:             regapi.NewID("app.deps", "addon"),
			Kind:           regapi.NamespaceDependency,
			DependencyRoot: true,
			Meta: attrs.NewBagFrom(map[string]any{
				fixtureModuleKey: "acme/deployment", fixtureModuleVersionKey: "v1.0.0",
			}),
			Data: payload.New(map[string]any{"component": "acme/addon", "version": "v1.0.0"}),
		}
		_, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: addonRoot}})
		require.NoError(t, err)
		_, err = reg.GetEntry(regapi.NewID("acme.addon", "service"))
		require.NoError(t, err, "fixture must install the departing module before rollback")
		require.True(t, packRegistry.HasModulePack("acme/addon", "v1.0.0"))
		runner.transitions = nil

		require.NoError(t, reg.ApplyVersion(ctx, baselineVersion))
		var rollbackOps []regapi.Operation
		for _, transition := range runner.transitions {
			rollbackOps = append(rollbackOps, transition...)
		}
		deleted := make(map[regapi.ID]struct{}, 2)
		for _, op := range rollbackOps {
			require.NotEqual(t, hostID, op.Entry.ID)
			if op.Kind == regapi.EntryDelete {
				deleted[op.Entry.ID] = struct{}{}
			}
		}
		require.Len(t, deleted, 2, "rollback must delete only the departing root and module entry")
		require.Contains(t, deleted, addonRoot.ID)
		require.Contains(t, deleted, regapi.NewID("acme.addon", "service"))
		_, err = reg.GetEntry(regapi.NewID("app.security", "admin_all_access"))
		require.NoError(t, err, "rollback must preserve entries owned by the deployment package")
		_, err = reg.GetEntry(regapi.NewID("app.deps", "app"))
		require.NoError(t, err, "an owned deployment root must remain present after rollback")
		_, err = reg.GetEntry(addonRoot.ID)
		require.Error(t, err)
		_, err = reg.GetEntry(regapi.NewID("acme.addon", "service"))
		require.Error(t, err)
		require.False(t, packRegistry.HasModulePack("acme/addon", "v1.0.0"), "rollback must unregister the departing embedded pack")

		_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: addonRoot}})
		require.NoError(t, err, "a follow-up install must succeed after rollback")
		_, err = reg.GetEntry(regapi.NewID("acme.addon", "service"))
		require.NoError(t, err)
		require.True(t, packRegistry.HasModulePack("acme/addon", "v1.0.0"))
	})

	t.Run("restores authored host drift without derived rewrite", func(t *testing.T) {
		packRegistry := embedpkg.NewRegistry()
		defer func() { require.NoError(t, packRegistry.Close()) }()
		ctx := embedapi.WithRegistry(newTestContext(), packRegistry)
		reg, runner, baselineVersion, hostID := newLegacyArtifactRollbackRegistry(ctx, t)

		current, err := reg.GetEntry(hostID)
		require.NoError(t, err)
		changed := current
		changed.Data = payload.New(map[string]any{
			"host":      map[string]any{"max_processes": 500, "workers": 16},
			"lifecycle": map[string]any{"auto_start": true},
		})
		_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: changed}})
		require.NoError(t, err)
		runner.transitions = nil

		require.NoError(t, reg.ApplyVersion(ctx, baselineVersion))
		var hostOps []regapi.Operation
		for _, transition := range runner.transitions {
			for _, op := range transition {
				if op.Entry.ID == hostID {
					hostOps = append(hostOps, op)
				}
			}
		}
		require.Len(t, hostOps, 1)
		require.Equal(t, regapi.EntryUpdate, hostOps[0].Kind)
		hostData, ok := hostOps[0].Entry.Data.Data().(map[string]any)
		require.True(t, ok)
		hostConfig, ok := hostData["host"].(map[string]any)
		require.True(t, ok)
		require.EqualValues(t, 8, hostConfig["workers"])
	})
}

func newLegacyArtifactRollbackRegistry(
	ctx context.Context,
	t *testing.T,
) (*registryimpl.Reg, *bootRecordingRunner, regapi.Version, regapi.ID) {
	t.Helper()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")
	lockPath := filepath.Join(tmpDir, "wippy.lock")
	hostID := regapi.NewID("keeper.gov", "processes")
	hostData := map[string]any{
		"host":      map[string]any{"max_processes": 500, "workers": 8},
		"lifecycle": map[string]any{"auto_start": true},
	}
	artifacts := map[string][]byte{
		"app": buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.app", "marker"), Kind: regapi.EntryKind,
			Data: map[string]any{"enabled": true},
		}}),
		"runtime": buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID(hostID.NS, hostID.Name), Kind: "process.host", Data: hostData,
		}}),
		"addon": buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.addon", "service"), Kind: regapi.EntryKind,
			Data: map[string]any{"enabled": true},
		}}),
	}
	digests := make(map[string]string, len(artifacts))
	for module, artifact := range artifacts {
		sum := sha256.Sum256(artifact)
		digests[module] = "sha256:" + hex.EncodeToString(sum[:])
	}
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`directories:
  modules: vendor
modules:
  - name: acme/app
    version: v1.0.0
    hash: %s
  - name: acme/runtime
    version: v1.0.0
    hash: %s
`, digests["app"], digests["runtime"])), 0o600))

	fixturePath := filepath.Join(tmpDir, "runtime-fixture.wapp")
	require.NoError(t, os.WriteFile(fixturePath, artifacts["runtime"], 0o600))
	loadedRuntime, err := loadEntriesFromWapp(fixturePath)
	require.NoError(t, err)
	require.Len(t, loadedRuntime, 1)
	legacyHost := loadedRuntime[0]
	legacyHost.Meta = attrs.NewBagFrom(map[string]any{
		fixtureModuleKey: "acme/runtime", fixtureModuleVersionKey: "v1.0.0",
	})

	hubClient := &fakeHub{
		getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
			artifact, ok := artifacts[module]
			if !ok {
				return nil, fmt.Errorf("unexpected module %s/%s", org, module)
			}
			manifest := &ModuleManifest{
				Org: org, Name: module, Version: "v1.0.0", VersionID: "v1.0.0",
				Digest: digests[module], SizeBytes: uint64(len(artifact)), URL: "memory://" + module,
			}
			if module == "app" {
				manifest.Dependencies = []ManifestDep{{Org: "acme", Name: "runtime", Version: "v1.0.0"}}
			}
			return manifest, nil
		},
		downloadFile: func(_ context.Context, url, dest string) error {
			module := strings.TrimPrefix(url, "memory://")
			artifact, ok := artifacts[module]
			if !ok {
				return fmt.Errorf("unexpected artifact %q", url)
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			return os.WriteFile(dest, artifact, 0o600)
		},
	}
	resolver := topology.NewResolver()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: hubClient, Logger: zap.NewNop(), Resolver: resolver,
		LockPath: lockPath, VendorDir: vendorDir,
	})
	require.NoError(t, err)
	runner := &bootRecordingRunner{}
	prov := newRegistryProvenance(regapi.ProvenancedState{})
	reg := registryimpl.NewRegistry(
		historymem.New(), runner, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop(),
		registryimpl.WithKindDirective(
			regapi.NamespaceDependency,
			regexp.NewDependencyDirective(prov.expand(handler)).WithResolutionTransition(prov.reconcile(handler)),
		),
	)
	root := regapi.Entry{
		ID: regapi.NewID("app.deps", "app"), Kind: regapi.NamespaceDependency,
		DependencyRoot: true,
		Meta: attrs.NewBagFrom(map[string]any{
			fixtureModuleKey: "acme/deployment", fixtureModuleVersionKey: "v1.0.0",
		}),
		Data: payload.New(map[string]any{"component": "acme/app", "version": "v1.0.0"}),
	}
	baseline := regapi.State{
		root,
		{
			ID: regapi.NewID("app.security", "admin_all_access"), Kind: "security.policy",
			Meta: attrs.NewBagFrom(map[string]any{
				fixtureModuleKey: "acme/deployment", fixtureModuleVersionKey: "v1.0.0",
			}),
			Data: payload.New(map[string]any{"allow": true}),
		},
		{
			ID: regapi.NewID("acme.app", "marker"), Kind: regapi.EntryKind,
			Meta: attrs.NewBagFrom(map[string]any{
				fixtureModuleKey: "acme/app", fixtureModuleVersionKey: "v1.0.0",
			}),
			Data: payload.New(map[string]any{"enabled": true}),
		},
		legacyHost,
	}
	baselineVersion := version.FromParent(nil, 0)
	provenancedBaseline := fixtureState(baseline)
	prov.seed(provenancedBaseline)
	require.NoError(t, reg.LoadState(ctx, provenancedBaseline.Entries, baselineVersion))
	_, err = reg.GetEntry(hostID)
	require.NoError(t, err)
	require.Empty(t, prov.record(hostID).Digest, "fixture requires a host entry with no resident artifact identity")
	runner.transitions = nil
	return reg, runner, baselineVersion, hostID
}

type bootDirectiveFunc func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error)

func (f bootDirectiveFunc) Expand(ctx context.Context, op regapi.Operation, state regapi.State) (regapi.DirectiveResult, error) {
	return f(ctx, op, state)
}

func (r *bootRecordingRunner) Transition(_ context.Context, state regapi.State, changes regapi.ChangeSet) (regapi.State, error) {
	r.transitions = append(r.transitions, append(regapi.ChangeSet(nil), changes...))
	stateMap := topology.NewStateMap(state)
	for _, op := range changes {
		switch op.Kind {
		case regapi.EntryCreate, regapi.EntryUpdate:
			stateMap[op.Entry.ID] = op.Entry
		case regapi.EntryDelete:
			delete(stateMap, op.Entry.ID)
		}
	}
	return topology.StateMapToSlice(stateMap), nil
}

func TestDependencyHandler_BootExpandsSourceRootBeforeUnrelatedInstall(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	artifacts := map[string][]byte{
		"analysis": buildWappBytes(t, []wapp.Entry{{
			ID:   wapp.NewID("acme.analysis", "runtime"),
			Kind: "process.host",
			Data: map[string]any{"runtime": "wasm"},
		}}),
		"crm": buildWappBytes(t, []wapp.Entry{{
			ID:   wapp.NewID("acme.crm", "service"),
			Kind: "service",
			Data: map[string]any{"enabled": true},
		}}),
	}
	downloads := map[string]int{}

	hubClient := &fakeHub{
		getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
			if _, ok := artifacts[module]; !ok {
				return nil, fmt.Errorf("unknown module %s/%s", org, module)
			}
			return &ModuleManifest{
				Org: org, Name: module, Version: "v1.0.0", URL: "memory://" + module,
			}, nil
		},
		downloadFile: func(_ context.Context, url, destPath string) error {
			module := url[len("memory://"):]
			artifact, ok := artifacts[module]
			if !ok {
				return fmt.Errorf("unknown artifact %q", url)
			}
			downloads[module]++
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}
			return os.WriteFile(destPath, artifact, 0o600)
		},
	}

	resolver := topology.NewResolver()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       hubClient,
		Logger:    zap.NewNop(),
		Resolver:  resolver,
		LockPath:  filepath.Join(tmpDir, "wippy.lock"),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	runner := &bootRecordingRunner{}
	reg := registryimpl.NewRegistry(
		historymem.New(),
		runner,
		topology.NewStateBuilder(zap.NewNop(), resolver),
		resolver,
		zap.NewNop(),
		registryimpl.WithKindDirective(regapi.NamespaceDependency, bootDirectiveFunc(newRegistryProvenance(regapi.ProvenancedState{}).expand(handler))),
	)

	analysisRoot := regapi.Entry{
		ID:   regapi.NewID("app.deps", "analysis"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/analysis","version":"v1.0.0"}`, payload.JSON),
	}
	require.NoError(t, reg.LoadState(ctx, regapi.State{analysisRoot}, version.FromParent(nil, 0)))

	_, err = reg.GetEntry(regapi.NewID("acme.analysis", "runtime"))
	require.NoError(t, err, "boot must not publish a dependency root without its module entries")
	require.Equal(t, 1, downloads["analysis"], "the declared module must be installed during boot")

	runner.transitions = nil
	crmRoot := regapi.Entry{
		ID:   regapi.NewID("app.deps", "crm"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/crm","version":"v1.0.0"}`, payload.JSON),
	}
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: crmRoot}})
	require.NoError(t, err)
	require.Equal(t, 1, downloads["analysis"], "installing CRM must not materialize or reinstall Analysis")
	require.Equal(t, 1, downloads["crm"])

	for _, transition := range runner.transitions {
		for _, op := range transition {
			require.NotEqual(t, regapi.NewID("acme.analysis", "runtime"), op.Entry.ID,
				"an unrelated install must not mutate the module established at boot")
		}
	}
}
