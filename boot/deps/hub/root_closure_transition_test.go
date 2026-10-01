// SPDX-License-Identifier: MIT
package hub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	expansion "github.com/wippyai/runtime/system/registry/expansion"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

type beeRootClosureDependency struct {
	Component string `json:"component"`
	Version   string `json:"version"`
}

// A Bee release changes both the application and its nested pack versions.
func TestBeeDeploymentRootUpdateChangesNestedVersions(t *testing.T) {
	ctx := newTestContext()
	directory := t.TempDir()
	lockPath := filepath.Join(directory, "wippy.lock")
	vendor := filepath.Join(directory, "vendor")
	workerID := regapi.NewID("deployment.modules", "worker")
	obsoleteID := regapi.NewID("deployment.modules", "obsolete")
	type selection struct{ module, version string }
	artifacts := make(map[selection][]byte)
	digests := make(map[selection]string)
	for _, version := range []string{"1.0.0", "2.0.0"} {
		for _, name := range []string{"app", "worker", "obsolete"} {
			entries := []wapp.Entry{{ID: wapp.NewID("acme."+name, "definition"), Kind: regapi.NamespaceDefinition}}
			if name == "app" {
				entries = append(entries, wapp.Entry{ID: wapp.NewID(workerID.NS, workerID.Name),
					Kind: regapi.NamespaceDependency, Data: beeRootClosureDependency{"acme/worker", version}})
				if version == "1.0.0" {
					entries = append(entries, wapp.Entry{ID: wapp.NewID(obsoleteID.NS, obsoleteID.Name),
						Kind: regapi.NamespaceDependency, Data: beeRootClosureDependency{"acme/obsolete", version}})
				}
			}
			selected := selection{"acme/" + name, version}
			artifact := buildWappBytes(t, entries)
			artifacts[selected] = artifact
			digests[selected] = fmt.Sprintf("sha256:%x", sha256.Sum256(artifact))
		}
	}
	require.NoError(t, os.MkdirAll(filepath.Join(vendor, "acme"), 0o700))
	for _, name := range []string{"app", "worker", "obsolete"} {
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
`, digests[selection{"acme/app", "1.0.0"}], digests[selection{"acme/worker", "1.0.0"}], digests[selection{"acme/obsolete", "1.0.0"}])), 0o600))

	client := &fakeHub{
		getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
			selected := selection{org + "/" + module, version}
			artifact, found := artifacts[selected]
			if !found {
				return nil, fmt.Errorf("unexpected manifest %s@%s", selected.module, selected.version)
			}
			manifest := &ModuleManifest{Org: org, Name: module, Version: version, VersionID: version,
				Digest: digests[selected], SizeBytes: uint64(len(artifact)), URL: "memory://" + module + "@" + version}
			if module == "app" {
				manifest.Dependencies = []ManifestDep{{Org: "acme", Name: "worker", Version: version,
					Constraint: version, Digest: digests[selection{"acme/worker", version}]}}
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
	baseline := regapi.State{
		ownedEntry(regapi.Entry{ID: obsoleteID, Kind: regapi.NamespaceDependency, Registry: regapi.EntryMetadata{Root: true},
			Data: payload.New(beeRootClosureDependency{"acme/obsolete", "1.0.0"})}, "acme/app"),
		ownedEntry(regapi.Entry{ID: regapi.NewID("acme.obsolete", "definition"), Kind: regapi.NamespaceDefinition}, "acme/obsolete"),
		ownedEntry(regapi.Entry{ID: workerID, Kind: regapi.NamespaceDependency,
			Registry: regapi.EntryMetadata{Root: true},
			Data:     payload.New(beeRootClosureDependency{"acme/worker", "1.0.0"})}, "acme/app"),
		ownedEntry(regapi.Entry{ID: regapi.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition}, "acme/app"),
		ownedEntry(regapi.Entry{ID: regapi.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition}, "acme/worker"),
	}
	root := regapi.Entry{ID: regapi.NewID("deployment.packages", "application"), Kind: regapi.NamespaceDependency,
		Data: payload.New(beeRootClosureDependency{"acme/app", "2.0.0"})}
	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryUpdate, Entry: root}, baseline)
	require.NoError(t, err, "the new application owns the nested 2.0.0 declaration; its old 1.0.0 declaration cannot constrain the new closure")
	require.NotNil(t, result.Resolution)
	require.Equal(t, "2.0.0", resolutionModuleVersion(result.Resolution, "acme/app"))
	require.Equal(t, "2.0.0", resolutionModuleVersion(result.Resolution, "acme/worker"))

	t.Run("independent root still constrains selection", func(t *testing.T) {
		independent := regapi.Entry{ID: regapi.NewID("host.modules", "worker"), Kind: regapi.NamespaceDependency,
			Data: payload.New(beeRootClosureDependency{"acme/worker", "1.0.0"})}
		_, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: root}, append(baseline, independent))
		require.ErrorContains(t, err, "dependency resolution failed")
	})
	t.Run("nested update does not release its own constraint", func(t *testing.T) {
		changed := ownedEntry(regapi.Entry{ID: workerID, Kind: regapi.NamespaceDependency,
			Registry: regapi.EntryMetadata{Root: true}}, "acme/app")
		changed.Data = payload.New(beeRootClosureDependency{"acme/worker", "2.0.0"})
		parent := regapi.Entry{ID: root.ID, Kind: regapi.NamespaceDependency,
			Data: payload.New(beeRootClosureDependency{"acme/app", "1.0.0"})}
		_, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryUpdate, Entry: changed}, append(baseline, parent))
		require.ErrorContains(t, err, "dependency resolution failed")
	})
	history := historymem.New()
	newRegistry := func() *registryimpl.Reg {
		resolver := topology.NewResolver()
		dependencyHandler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: client, Logger: zap.NewNop(),
			Resolver: resolver, LockPath: lockPath, VendorDir: vendor})
		require.NoError(t, err)
		return registryimpl.NewRegistry(history, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver),
			resolver, zap.NewNop(), registryimpl.WithKindDirective(regapi.NamespaceDependency,
				expansion.NewDependencyDirective(dependencyHandler.Expand).WithChangesExpansion(dependencyHandler.ExpandChanges).
					WithResolutionTransition(dependencyHandler.ReconcileResolution)))
	}
	reg := newRegistry()
	v0 := version.FromParent(nil, regapi.RootVersion)
	require.NoError(t, reg.LoadState(ctx, baseline, v0))
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: root}})
	require.NoError(t, err)
	assertSelected := func(reg *registryimpl.Reg, expected string) {
		require.Equal(t, expected, snapshotModuleVersion(t, reg, "acme/app"))
		require.Equal(t, expected, snapshotModuleVersion(t, reg, "acme/worker"))
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
			require.Equal(t, expected, snapshotModuleVersion(t, reg, "acme/obsolete"))
		} else {
			require.Error(t, obsoleteError)
			_, err := reg.GetEntry(regapi.NewID("acme.obsolete", "definition"))
			require.Error(t, err)
		}
	}
	assertSelected(reg, "2.0.0")
	stored, err := history.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Len(t, stored.Roots, 2)
	for _, declaration := range stored.Roots {
		require.Equal(t, "2.0.0", declaration.Version)
	}
	require.NoError(t, reg.ApplyVersion(ctx, v0))
	assertSelected(reg, "1.0.0")
	require.NoError(t, reg.ApplyVersion(ctx, v1))
	assertSelected(reg, "2.0.0")
	client.getManifest = func(context.Context, string, string, string) (*ModuleManifest, error) {
		return nil, fmt.Errorf("network disabled")
	}
	client.downloadFile = func(context.Context, string, string) error {
		return fmt.Errorf("network disabled")
	}
	restarted := newRegistry()
	require.NoError(t, restarted.LoadState(regapi.WithRegistry(regapi.WithDependencyAccess(ctx, regapi.DependencyAccessVerifiedOffline), restarted), baseline, v1))
	assertSelected(restarted, "2.0.0")
}
