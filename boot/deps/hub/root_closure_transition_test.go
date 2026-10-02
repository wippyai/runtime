// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	expansion "github.com/wippyai/runtime/system/registry/expansion"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

type rootClosureDependency struct {
	Component string `json:"component"`
	Version   string `json:"version"`
}

// A deployment root release must publish nested definitions with their selected
// versions, while leaving independent resident modules untouched.
func TestDeploymentRootUpdateChangesNestedVersionsAndDefinitions(t *testing.T) {
	for _, preserveObsolete := range []bool{false, true} {
		t.Run(fmt.Sprintf("independent_obsolete_root=%t", preserveObsolete), func(t *testing.T) {
			testDeploymentRootClosure(t, preserveObsolete)
		})
	}
}

func testDeploymentRootClosure(t *testing.T, preserveObsolete bool) {
	ctx := newTestContext()
	directory := t.TempDir()
	lockPath := filepath.Join(directory, "wippy.lock")
	vendor := filepath.Join(directory, "vendor")
	workerID := regapi.NewID("deployment.modules", "worker")
	obsoleteID := regapi.NewID("deployment.modules", "obsolete")
	helperID := regapi.NewID("worker.modules", "helper")
	type selection struct{ module, version string }
	artifacts := make(map[selection][]byte)
	digests := make(map[selection]string)
	for _, version := range []string{"1.0.0", "2.0.0"} {
		for _, name := range []string{"app", "worker", "obsolete", "helper"} {
			entries := []wapp.Entry{{ID: wapp.NewID("acme."+name, "definition"), Kind: regapi.NamespaceDefinition,
				Data: map[string]any{"source": "return " + version}}}
			if name == "app" {
				entries = append(entries, wapp.Entry{ID: wapp.NewID(workerID.NS, workerID.Name),
					Kind: regapi.NamespaceDependency, Data: rootClosureDependency{"acme/worker", version}})
				if version == "1.0.0" {
					entries = append(entries, wapp.Entry{ID: wapp.NewID(obsoleteID.NS, obsoleteID.Name),
						Kind: regapi.NamespaceDependency, Data: rootClosureDependency{"acme/obsolete", version}})
				}
			}
			if name == "worker" {
				entries = append(entries, wapp.Entry{ID: wapp.NewID(helperID.NS, helperID.Name),
					Kind: regapi.NamespaceDependency, Data: rootClosureDependency{"acme/helper", version}})
			}
			selected := selection{"acme/" + name, version}
			artifacts[selected] = buildWappBytes(t, entries)
		}
	}
	// A validly hashed artifact that contradicts its manifest must still be
	// rejected after loading; releasing old declarations is not a bypass.
	inconsistent := selection{"acme/app", "3.0.0"}
	artifacts[inconsistent] = buildWappBytes(t, []wapp.Entry{
		{ID: wapp.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition},
		{ID: wapp.NewID(workerID.NS, workerID.Name), Kind: regapi.NamespaceDependency,
			Data: rootClosureDependency{"acme/worker", "1.0.0"}},
	})
	artifacts[selection{"acme/worker", "3.0.0"}] = buildWappBytes(t, []wapp.Entry{
		{ID: wapp.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition},
		{ID: wapp.NewID(helperID.NS, helperID.Name), Kind: regapi.NamespaceDependency,
			Data: rootClosureDependency{"acme/helper", "1.0.0"}},
	})
	artifacts[selection{"acme/app", "4.0.0"}] = buildWappBytes(t, []wapp.Entry{
		{ID: wapp.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition},
		{ID: wapp.NewID(workerID.NS, workerID.Name), Kind: regapi.NamespaceDependency,
			Data: rootClosureDependency{"acme/worker", "3.0.0"}},
	})
	for selected, artifact := range artifacts {
		digests[selected] = fmt.Sprintf("sha256:%x", sha256.Sum256(artifact))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(vendor, "acme"), 0o700))
	for _, name := range []string{"app", "worker", "obsolete", "helper"} {
		require.NoError(t, os.WriteFile(filepath.Join(vendor, "acme", name+"-1.0.0.wapp"),
			artifacts[selection{"acme/" + name, "1.0.0"}], 0o600))
	}
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`directories:
  modules: vendor
modules:
  - name: acme/app
    version: 1.0.0
    hash: %s
    root: true
  - name: acme/worker
    version: 1.0.0
    hash: %s
  - name: acme/obsolete
    version: 1.0.0
    hash: %s
  - name: acme/helper
    version: 1.0.0
    hash: %s
`, digests[selection{"acme/app", "1.0.0"}], digests[selection{"acme/worker", "1.0.0"}], digests[selection{"acme/obsolete", "1.0.0"}], digests[selection{"acme/helper", "1.0.0"}])), 0o600))

	client := &fakeHub{
		getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
			selected := selection{org + "/" + module, version}
			artifact, found := artifacts[selected]
			if !found {
				return nil, fmt.Errorf("unexpected manifest %s@%s", selected.module, selected.version)
			}
			manifest := &ModuleManifest{Org: org, Name: module, Version: version, VersionID: version,
				Digest: digests[selected], SizeBytes: uint64(len(artifact)), URL: "memory://" + module + "@" + version}
			if module == "worker" {
				helperVersion := version
				if version == "3.0.0" {
					helperVersion = "2.0.0"
				}
				manifest.Dependencies = []ManifestDep{{Org: "acme", Name: "helper", Version: helperVersion,
					Constraint: helperVersion, Digest: digests[selection{"acme/helper", helperVersion}]}}
			}
			if module == "app" {
				workerVersion := version
				switch version {
				case "3.0.0":
					workerVersion = "2.0.0"
				case "4.0.0":
					workerVersion = "3.0.0"
				}
				manifest.Dependencies = []ManifestDep{{Org: "acme", Name: "worker", Version: workerVersion,
					Constraint: workerVersion, Digest: digests[selection{"acme/worker", workerVersion}]}}
				if version == "1.0.0" {
					manifest.Dependencies = append(manifest.Dependencies, ManifestDep{Org: "acme", Name: "obsolete", Version: version,
						Constraint: version, Digest: digests[selection{"acme/obsolete", version}]})
				}
			}
			return manifest, nil
		},
		downloadFile: func(_ context.Context, url, destination string) error {
			for selected, artifact := range artifacts {
				if url == "memory://"+selected.module[len("acme/"):]+"@"+selected.version {
					if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
						return err
					}
					return os.WriteFile(destination, artifact, 0o600)
				}
			}
			return fmt.Errorf("unexpected download %s", url)
		},
	}
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: client, Logger: zap.NewNop(),
		Resolver: topology.NewResolver(), LockPath: lockPath, VendorDir: vendor})
	require.NoError(t, err)
	t.Cleanup(handler.manifestCache.Close)
	baseline := regapi.State{
		ownedEntry(regapi.Entry{ID: obsoleteID, Kind: regapi.NamespaceDependency, Registry: regapi.EntryMetadata{Root: true},
			Data: payload.New(rootClosureDependency{"acme/obsolete", "1.0.0"})}, "acme/app"),
		ownedEntry(regapi.Entry{ID: regapi.NewID("acme.obsolete", "definition"), Kind: regapi.NamespaceDefinition,
			Data: payload.New(map[string]any{"source": "resident independent source"})}, "acme/obsolete"),
		ownedEntry(regapi.Entry{ID: workerID, Kind: regapi.NamespaceDependency,
			Registry: regapi.EntryMetadata{Root: true},
			Data:     payload.New(rootClosureDependency{"acme/worker", "1.0.0"})}, "acme/app"),
		ownedEntry(regapi.Entry{ID: regapi.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition,
			Data: payload.New(map[string]any{"source": "return 1.0.0"})}, "acme/app"),
		ownedEntry(regapi.Entry{ID: regapi.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition,
			Data: payload.New(map[string]any{"source": "return 1.0.0"})}, "acme/worker"),
		ownedEntry(regapi.Entry{ID: helperID, Kind: regapi.NamespaceDependency, Registry: regapi.EntryMetadata{Root: true},
			Data: payload.New(rootClosureDependency{"acme/helper", "1.0.0"})}, "acme/worker"),
		ownedEntry(regapi.Entry{ID: regapi.NewID("acme.helper", "definition"), Kind: regapi.NamespaceDefinition,
			Data: payload.New(map[string]any{"source": "return 1.0.0"})}, "acme/helper"),
	}
	root := regapi.Entry{ID: regapi.NewID("deployment.packages", "application"), Kind: regapi.NamespaceDependency,
		Data: payload.New(rootClosureDependency{"acme/app", "2.0.0"})}
	previousRoot := root
	previousRoot.Data = payload.New(rootClosureDependency{"acme/app", "1.0.0"})
	baseline = append(baseline, previousRoot)
	if preserveObsolete {
		baseline = append(baseline, regapi.Entry{ID: regapi.NewID("host.modules", "obsolete"), Kind: regapi.NamespaceDependency,
			Data: payload.New(rootClosureDependency{"acme/obsolete", "1.0.0"})})
	}
	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryUpdate, Entry: root}, baseline)
	require.NoError(t, err, "the new application owns the nested 2.0.0 declaration; its old 1.0.0 declaration cannot constrain the new closure")
	require.NotNil(t, result.Resolution)
	require.Equal(t, "2.0.0", resolutionModuleVersion(result.Resolution, "acme/app"))
	require.Equal(t, "2.0.0", resolutionModuleVersion(result.Resolution, "acme/worker"))
	require.Equal(t, "2.0.0", resolutionModuleVersion(result.Resolution, "acme/helper"))

	for _, component := range []string{"worker", "helper"} {
		t.Run("independent_pin_"+component, func(t *testing.T) {
			independent := regapi.Entry{ID: regapi.NewID("host.modules", component), Kind: regapi.NamespaceDependency,
				Data: payload.New(rootClosureDependency{"acme/" + component, "1.0.0"})}
			_, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryUpdate, Entry: root}, append(slices.Clone(baseline), independent))
			require.ErrorContains(t, err, "dependency resolution failed")
		})
	}
	t.Run("nested update does not release its own constraint", func(t *testing.T) {
		changed := ownedEntry(regapi.Entry{ID: workerID, Kind: regapi.NamespaceDependency,
			Registry: regapi.EntryMetadata{Root: true}}, "acme/app")
		changed.Data = payload.New(rootClosureDependency{"acme/worker", "2.0.0"})
		parent := regapi.Entry{ID: root.ID, Kind: regapi.NamespaceDependency,
			Data: payload.New(rootClosureDependency{"acme/app", "1.0.0"})}
		_, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryUpdate, Entry: changed}, applyOperationToState(baseline, regapi.Operation{Kind: regapi.EntryUpdate, Entry: parent}))
		require.ErrorContains(t, err, "dependency resolution failed")
	})
	historyPath := filepath.Join(directory, "history.db")
	history, err := historysqlite.NewSQLite(historyPath, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() {
		if history != nil {
			require.NoError(t, history.Close())
		}
	})
	newRegistry := func() *registryimpl.Reg {
		resolver := topology.NewResolver()
		dependencyHandler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: client, Logger: zap.NewNop(),
			Resolver: resolver, LockPath: lockPath, VendorDir: vendor})
		require.NoError(t, err)
		t.Cleanup(dependencyHandler.manifestCache.Close)
		return registryimpl.NewRegistry(history, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver),
			resolver, zap.NewNop(), registryimpl.WithKindDirective(regapi.NamespaceDependency,
				expansion.NewDependencyDirective(dependencyHandler.Expand).WithChangesExpansion(dependencyHandler.ExpandChanges).
					WithResolutionTransition(dependencyHandler.ReconcileResolution)))
	}
	reg := newRegistry()
	v0 := version.FromParent(nil, regapi.RootVersion)
	require.NoError(t, reg.LoadState(ctx, baseline, v0))
	for _, test := range []struct {
		version string
		invalid bool
	}{{"2.0.0", false}, {"3.0.0", true}, {"4.0.0", true}} {
		t.Run("deployment_reconciliation_"+test.version, func(t *testing.T) {
			before := reg.Snapshot()
			changed := root
			changed.Registry.Root = true // A new source deployment, not a history edit.
			changed.Data = payload.New(rootClosureDependency{"acme/app", test.version})
			target := applyOperationToState(before.Entries, regapi.Operation{Kind: regapi.EntryUpdate, Entry: changed})
			result, err := handler.ReconcileResolution(regapi.WithRegistry(ctx, reg), before.Entries, target, before.Registry.Resolution)
			if test.invalid {
				require.ErrorContains(t, err, "selected module does not satisfy materialized declaration")
			} else {
				require.NoError(t, err)
				require.Equal(t, "2.0.0", resolutionModuleVersion(result.Resolution, "acme/helper"))
			}
			require.Equal(t, before, reg.Snapshot(), "planning reconciliation must not publish state")
		})
	}
	if preserveObsolete {
		resident, err := reg.GetEntry(regapi.NewID("acme.obsolete", "definition"))
		require.NoError(t, err)
		resident.Data = payload.New(map[string]any{"source": "resident independent source"})
		v0, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: resident}})
		require.NoError(t, err)
	}
	assertRejectedUpdate := func(t *testing.T, changes regapi.ChangeSet, message string) {
		t.Helper()
		before := reg.Snapshot()
		beforeResolution, err := history.GetDependencyResolution(before.Version)
		require.NoError(t, err)
		_, err = reg.Apply(ctx, changes)
		require.ErrorContains(t, err, message)
		require.Equal(t, before, reg.Snapshot(), "a rejected update must not publish any partial state")
		head, err := history.Head()
		require.NoError(t, err)
		require.Equal(t, before.Version.ID(), head.ID(), "a rejected update must not advance history")
		afterResolution, err := history.GetDependencyResolution(head)
		require.NoError(t, err)
		require.Equal(t, beforeResolution, afterResolution, "a rejected update must not rebind its checkpoint")
	}
	for _, test := range []struct{ version, message string }{
		{"3.0.0", "selected module does not satisfy materialized declaration"},
		{"4.0.0", "selected module does not satisfy materialized declaration"},
		{"5.0.0", "unexpected manifest"},
	} {
		t.Run("rejected_release_"+test.version, func(t *testing.T) {
			changed := root
			changed.Data = payload.New(rootClosureDependency{"acme/app", test.version})
			assertRejectedUpdate(t, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: changed}}, test.message)
		})
	}
	for _, component := range []string{"worker", "helper"} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("failed_batch_%s_reverse=%t", component, reverse), func(t *testing.T) {
				independent := regapi.Entry{ID: regapi.NewID("host.modules", component), Kind: regapi.NamespaceDependency,
					Data: payload.New(rootClosureDependency{"acme/" + component, "1.0.0"})}
				changes := regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: independent}, {Kind: regapi.EntryUpdate, Entry: root}}
				if reverse {
					slices.Reverse(changes)
				}
				assertRejectedUpdate(t, changes, "dependency resolution failed")
			})
		}
	}
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: root}})
	require.NoError(t, err)
	assertSelected := func(reg *registryimpl.Reg, expected string) {
		for _, module := range []string{"app", "worker", "helper"} {
			snapshot := reg.Snapshot()
			index := slices.IndexFunc(snapshot.Entries, func(entry regapi.Entry) bool {
				return entry.ID == regapi.NewID("acme."+module, "definition")
			})
			require.NotEqual(t, -1, index)
			entry := snapshot.Entries[index]
			var data map[string]any
			require.NoError(t, payload.GetTranscoder(ctx).Unmarshal(entry.Data, &data))
			require.Equal(t, "return "+expected, data["source"], "selected %s must publish its definition", module)
		}
		require.Equal(t, expected, snapshotModuleVersion(t, reg, "acme/app"))
		require.Equal(t, expected, snapshotModuleVersion(t, reg, "acme/worker"))
		require.Equal(t, expected, snapshotModuleVersion(t, reg, "acme/helper"))
		nested, err := reg.GetEntry(workerID)
		require.NoError(t, err)
		require.True(t, nested.Registry.Root)
		require.Equal(t, "acme/app", entryModule(nested))
		definition, err := decodeDependency(ctx, payload.GetTranscoder(ctx), nested)
		require.NoError(t, err)
		require.Equal(t, expected, definition.Version)
		_, obsoleteError := reg.GetEntry(obsoleteID)
		if expected == "1.0.0" {
			require.NoError(t, obsoleteError)
			require.Equal(t, "1.0.0", snapshotModuleVersion(t, reg, "acme/obsolete"))
		} else {
			require.Error(t, obsoleteError)
			_, err := reg.GetEntry(regapi.NewID("acme.obsolete", "definition"))
			if preserveObsolete {
				require.NoError(t, err, "the independent installation retains the module, not the old app's declaration")
				require.Equal(t, "1.0.0", snapshotModuleVersion(t, reg, "acme/obsolete"))
				entry, err := reg.GetEntry(regapi.NewID("acme.obsolete", "definition"))
				require.NoError(t, err)
				var data map[string]any
				require.NoError(t, payload.GetTranscoder(ctx).Unmarshal(entry.Data, &data))
				require.Equal(t, "resident independent source", data["source"], "untouched independent module must retain its resident definition")
			} else {
				require.Error(t, err)
			}
		}
	}
	assertSelected(reg, "2.0.0")
	stored, err := history.GetDependencyResolution(v1)
	require.NoError(t, err)
	expectedRoots := 2
	if preserveObsolete {
		expectedRoots++
	}
	require.Len(t, stored.Roots, expectedRoots)
	for _, declaration := range stored.Roots {
		if declaration.Component == "acme/obsolete" {
			require.Equal(t, "1.0.0", declaration.Version)
		} else {
			require.Equal(t, "2.0.0", declaration.Version)
		}
	}
	// Reapplying the same selection may record a new version but must not
	// change the graph or mutate the previous version's checkpoint.
	previousVersion := v1
	v1, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: root}})
	require.NoError(t, err)
	assertSelected(reg, "2.0.0")
	unchanged, err := history.GetDependencyResolution(previousVersion)
	require.NoError(t, err)
	require.Equal(t, stored, unchanged)
	require.NoError(t, reg.ApplyVersion(ctx, v0))
	assertSelected(reg, "1.0.0")
	require.NoError(t, reg.ApplyVersion(ctx, v1))
	assertSelected(reg, "2.0.0")
	require.NoError(t, history.Close())
	history, err = historysqlite.NewSQLite(historyPath, zap.NewNop())
	require.NoError(t, err)
	client.getManifest = func(context.Context, string, string, string) (*ModuleManifest, error) {
		return nil, fmt.Errorf("network disabled")
	}
	client.downloadFile = func(context.Context, string, string) error {
		return fmt.Errorf("network disabled")
	}
	restarted := newRegistry()
	loadCtx := regapi.WithRegistry(regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline), restarted)
	err = restarted.LoadState(loadCtx, baseline, v1)
	require.NoError(t, err, "%+v", apierror.BuildChain(err))
	assertSelected(restarted, "2.0.0")
	reopenedResolution, err := history.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Equal(t, unchanged, reopenedResolution, "offline replay must retain the exact stored checkpoint")
	require.NoError(t, restarted.ApplyVersion(loadCtx, v0))
	assertSelected(restarted, "1.0.0")
	require.NoError(t, restarted.ApplyVersion(loadCtx, v1))
	assertSelected(restarted, "2.0.0")
}

func TestSolverDependenciesPreservesIndependentConstraints(t *testing.T) {
	declaration := func(id, owner, component string) desiredDependency {
		entry := regapi.Entry{ID: regapi.NewID("dependencies", id), Kind: regapi.NamespaceDependency}
		if owner != "" {
			entry = ownedEntry(entry, owner)
		}
		return desiredDependency{entry: entry, definition: DependencyDefinition{Component: component, Version: "1.0.0"}}
	}
	deps := []desiredDependency{
		declaration("app", "", "acme/app"),
		declaration("worker", "acme/app", "acme/worker"),
		declaration("helper", "acme/worker", "acme/helper"),
		declaration("leaf", "acme/helper", "acme/leaf"),
		declaration("addon", "", "acme/addon"),
		declaration("addon_leaf", "acme/addon", "acme/leaf"),
		declaration("worker_pin", "", "acme/worker"),
		declaration("helper_pin", "", "acme/helper"),
	}
	ids := func(input []desiredDependency) []string {
		result := make([]string, 0, len(input))
		for _, dep := range input {
			result = append(result, dep.entry.ID.Name)
		}
		return result
	}
	handler := &DependencyHandler{deployment: &regapi.Deployment{Root: "acme/app"}}
	for _, target := range []string{"", "acme/worker", "acme/addon"} {
		t.Run("unchanged_closure_"+target, func(t *testing.T) {
			got := handler.solverDependencies(deps, map[string]struct{}{target: {}})
			require.Equal(t, ids(deps), ids(got), "only selecting the deployment root releases its owned declarations")
		})
	}
	expected := []string{"app", "addon", "addon_leaf", "worker_pin", "helper_pin"}
	t.Run("deep_owned_closure", func(t *testing.T) {
		got := handler.solverDependencies(deps, map[string]struct{}{"acme/app": {}})
		require.Equal(t, expected, ids(got))
	})
	t.Run("declaration_order", func(t *testing.T) {
		reversed := slices.Clone(deps)
		slices.Reverse(reversed)
		want := slices.Clone(expected)
		slices.Reverse(want)
		got := handler.solverDependencies(reversed, map[string]struct{}{"acme/app": {}})
		require.Equal(t, want, ids(got))
	})
	t.Run("ownership_cycle", func(t *testing.T) {
		cyclic := append(slices.Clone(deps), declaration("cycle", "acme/helper", "acme/app"))
		got := handler.solverDependencies(cyclic, map[string]struct{}{"acme/app": {}})
		require.Equal(t, expected, ids(got), "closure traversal terminates without discarding independent roots")
	})
}
