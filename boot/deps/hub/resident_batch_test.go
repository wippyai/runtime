// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestExpandBatchKeepsUnselectedResidentModuleWithoutHub(t *testing.T) {
	for _, replaceRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("replace_root=%t", replaceRoot), func(t *testing.T) {
			vendor := filepath.Join(t.TempDir(), "vendor")
			workerID := regapi.NewID("deployment.dependencies", "worker")
			rootID := regapi.NewID("host.dependencies", "application")
			residentID := regapi.NewID("host.dependencies", "resident")
			for _, version := range []string{"1.0.0", "2.0.0"} {
				writeWapp(t, filepath.Join(vendor, "acme", "app-"+version+".wapp"), []wapp.Entry{
					{ID: wapp.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition},
					{ID: wapp.NewID(workerID.NS, workerID.Name), Kind: regapi.NamespaceDependency,
						Data: DependencyDefinition{Component: "acme/worker", Version: ">=1.0.0"}},
				})
			}
			writeWapp(t, filepath.Join(vendor, "acme", "worker-2.0.0.wapp"), []wapp.Entry{
				{ID: wapp.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition},
			})
			resident := regapi.ResolvedModule{Name: "acme/resident", Version: "1.0.0", Source: moduleSourceHub,
				Digest: "sha256:" + lockedResolutionDigest, VersionID: "resident-release"}
			ctx := withCurrentResolution(newTestContext(), resident,
				regapi.ResolvedModule{Name: "acme/app", Version: "1.0.0"},
				regapi.ResolvedModule{Name: "acme/worker", Version: "1.0.0"})
			handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{
				listVersions: func(_ context.Context, _, name string) ([]VersionInfo, error) {
					if name == "resident" {
						return nil, fmt.Errorf("unselected resident module must not list Hub versions")
					}
					return []VersionInfo{{Version: "1.0.0"}, {Version: "2.0.0"}}, nil
				},
				getManifest: func(_ context.Context, org, name, version string) (*ModuleManifest, error) {
					if name == "resident" {
						return nil, fmt.Errorf("unselected resident module is unavailable from the Hub")
					}
					manifest := &ModuleManifest{Org: org, Name: name, Version: version}
					if name == "app" {
						manifest.Dependencies = []ManifestDep{{Org: "acme", Name: "worker", Version: ">=1.0.0"}}
					}
					return manifest, nil
				},
			}, Logger: zap.NewNop(), VendorDir: vendor})
			require.NoError(t, err)
			t.Cleanup(handler.manifestCache.Close)
			handler.deployment = &regapi.Deployment{Root: "acme/app", Modules: []regapi.ResolvedModule{
				{Name: "acme/app", Version: "1.0.0"}, {Name: "acme/worker", Version: "1.0.0"},
			}}
			root := regapi.Entry{ID: rootID, Kind: regapi.NamespaceDependency,
				Data: payload.New(DependencyDefinition{Component: "acme/app", Version: "1.0.0"})}
			worker := ownedEntry(regapi.Entry{ID: workerID, Kind: regapi.NamespaceDependency,
				Registry: regapi.EntryMetadata{Root: true},
				Data:     payload.New(DependencyDefinition{Component: "acme/worker", Version: ">=1.0.0"})}, "acme/app")
			residentRoot := regapi.Entry{ID: residentID, Kind: regapi.NamespaceDependency,
				Data: payload.New(DependencyDefinition{Component: resident.Name, Version: ">=1.0.0"})}
			snapshot := regapi.State{root, worker, residentRoot,
				ownedEntry(regapi.Entry{ID: regapi.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition}, "acme/app"),
				ownedEntry(regapi.Entry{ID: regapi.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition}, "acme/worker"),
				ownedEntry(regapi.Entry{ID: regapi.NewID("acme.resident", "customized"), Kind: regapi.EntryKind,
					Data: payload.New(map[string]any{"value": "installation-owned"})}, resident.Name),
			}
			worker.Data = payload.New(DependencyDefinition{Component: "acme/worker", Version: "2.0.0"})
			changes := regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}}
			if replaceRoot {
				root.Data = payload.New(DependencyDefinition{Component: "acme/app", Version: "2.0.0"})
				changes = append(changes, regapi.Operation{Kind: regapi.EntryUpdate, Entry: root})
			}
			result, err := handler.ExpandChanges(ctx, changes, snapshot)
			require.NoError(t, err)
			require.True(t, result.Applied)
			for _, operation := range result.Additional {
				require.NotEqual(t, resident.Name, entryModule(operation.Operation.Entry), "resident entries must stay unchanged")
			}
			require.NotNil(t, result.Resolution)
			require.Contains(t, result.Resolution.Modules, resident, "the exact resident release identity must survive")
		})
	}
}

func TestResidentManifestResolutionUsesDeclarationsAndAllowsNewReleases(t *testing.T) {
	resident := regapi.ResolvedModule{Name: "acme/resident", Version: "v1.0.0", VersionID: "installed-release",
		Digest: "sha256:" + lockedResolutionDigest, SizeBytes: 42, Protected: true}
	state := regapi.State{ownedEntry(regapi.Entry{ID: regapi.NewID("acme.resident", "child"),
		Kind: regapi.NamespaceDependency,
		Data: payload.New(DependencyDefinition{Component: "acme/child", Version: ">=1.0.0"})}, resident.Name)}
	var manifests, listings int
	provider := newResidentManifestProvider(&fakeHub{
		getManifest: func(_ context.Context, org, name, version string) (*ModuleManifest, error) {
			manifests++
			require.Equal(t, "2.0.0", version)
			return &ModuleManifest{Org: org, Name: name, Version: version}, nil
		},
		listVersions: func(context.Context, string, string) ([]VersionInfo, error) {
			listings++
			return []VersionInfo{{Version: "2.0.0"}}, nil
		},
	}, []regapi.ResolvedModule{resident}, state)
	ctx := newTestContext()
	manifest, err := provider.GetManifest(ctx, "acme", "resident", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, resident.VersionID, manifest.VersionID)
	require.Equal(t, resident.Digest, manifest.Digest)
	require.Equal(t, resident.SizeBytes, manifest.SizeBytes)
	require.Equal(t, resident.Protected, manifest.Protected)
	require.Equal(t, []ManifestDep{{Org: "acme", Name: "child", Version: ">=1.0.0"}}, manifest.Dependencies)
	require.Zero(t, manifests)
	result, err := Resolve(ctx, provider, []DependencySpec{{Org: "acme", Name: "resident", Constraint: ">=2.0.0"}},
		&ResolveOptions{LockedVersions: map[string]string{resident.Name: resident.Version}})
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, "2.0.0", result.Modules[0].Version)
	require.Equal(t, 1, listings)
	require.Equal(t, 1, manifests)
	state[0].Data = payload.New(DependencyDefinition{Component: "invalid module", Version: "1.0.0"})
	provider = newResidentManifestProvider(provider, []regapi.ResolvedModule{resident}, state)
	_, err = provider.GetManifest(ctx, "acme", "resident", "1.0.0")
	require.Error(t, err)
	require.Equal(t, 1, manifests, "invalid resident declarations must surface without Hub fallback")
}
