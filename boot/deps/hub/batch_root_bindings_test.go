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

func TestExpandChangesRootReplacementPreservesBatchBindings(t *testing.T) {
	for _, rootFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("root_first=%t", rootFirst), func(t *testing.T) {
			ctx := newTestContext()
			vendor := filepath.Join(t.TempDir(), "vendor")
			moduleID := regapi.NewID("deployment.dependencies", "worker")
			rootID := regapi.NewID("host.dependencies", "application")
			writeWapp(t, filepath.Join(vendor, "acme", "app-2.0.0.wapp"), []wapp.Entry{
				{ID: wapp.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition},
				{ID: wapp.NewID(moduleID.NS, moduleID.Name), Kind: regapi.NamespaceDependency,
					Data: DependencyDefinition{Component: "acme/worker", Version: "2.0.0",
						Parameters: []Parameter{{Name: "declared", Value: "host:artifact"}, {Name: "introduced", Value: "host:introduced"}}}},
			})
			worker := []wapp.Entry{{ID: wapp.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition}}
			for _, name := range []string{"declared", "retained", "typed", "introduced"} {
				worker = append(worker,
					wapp.Entry{ID: wapp.NewID("acme.worker", name), Kind: regapi.NamespaceRequirement,
						Data: map[string]any{"targets": []any{map[string]any{"entry": "target", "path": ".meta." + name}}}},
				)
			}
			worker = append(worker, wapp.Entry{ID: wapp.NewID("acme.worker", "target"), Kind: regapi.EntryKind, Meta: map[string]any{}})
			writeWapp(t, filepath.Join(vendor, "acme", "worker-2.0.0.wapp"), worker)
			handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{
				getManifest: func(_ context.Context, org, name, constraint string) (*ModuleManifest, error) {
					manifest := &ModuleManifest{Org: org, Name: name, Version: constraint}
					if name == "app" {
						manifest.Dependencies = []ManifestDep{{Org: "acme", Name: "worker", Version: "2.0.0", Constraint: "2.0.0"}}
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
			module := ownedEntry(regapi.Entry{ID: moduleID, Kind: regapi.NamespaceDependency,
				Registry: regapi.EntryMetadata{Root: true}, Data: payload.New(DependencyDefinition{
					Component: "acme/worker", Version: "1.0.0", Parameters: []Parameter{
						{Name: "declared", Value: "host:old"}, {Name: "retained", Value: "host:scope"}, {Name: "typed", Value: true},
					},
				})}, "acme/app")
			changes := regapi.ChangeSet{
				{Kind: regapi.EntryUpdate, Entry: module}, {Kind: regapi.EntryUpdate, Entry: root},
			}
			changes[0].Entry.Data = payload.New(DependencyDefinition{Component: "acme/worker", Version: "2.0.0",
				Parameters: []Parameter{{Name: "declared", Value: "host:new"}, {Name: "retained", Value: "host:scope"}, {Name: "typed", Value: true}}})
			changes[1].Entry.Data = payload.New(DependencyDefinition{Component: "acme/app", Version: "2.0.0"})
			if rootFirst {
				changes[0], changes[1] = changes[1], changes[0]
			}
			snapshot := regapi.State{root, module,
				ownedEntry(regapi.Entry{ID: regapi.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition}, "acme/app"),
				ownedEntry(regapi.Entry{ID: regapi.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition}, "acme/worker"),
			}
			result, err := handler.ExpandChanges(ctx, changes, snapshot)
			require.NoError(t, err)
			require.True(t, result.Applied)
			found := false
			for _, scoped := range result.Additional {
				if scoped.Operation.Entry.ID == regapi.NewID("acme.worker", "target") {
					found = true
					require.Equal(t, "host:new", scoped.Operation.Entry.Meta["declared"])
					require.Equal(t, "host:scope", scoped.Operation.Entry.Meta["retained"])
					require.Equal(t, true, scoped.Operation.Entry.Meta["typed"])
					require.Equal(t, "host:introduced", scoped.Operation.Entry.Meta["introduced"])
				}
			}
			require.True(t, found)
		})
	}
}

func TestExpandClearedParametersDoNotInheritSnapshotBindings(t *testing.T) {
	ctx := newTestContext()
	vendor := filepath.Join(t.TempDir(), "vendor")
	writeWapp(t, filepath.Join(vendor, "acme", "worker-2.0.0.wapp"), []wapp.Entry{
		{ID: wapp.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition},
		{ID: wapp.NewID("acme.worker", "scope"), Kind: regapi.NamespaceRequirement,
			Data: map[string]any{"default": "module:default", "targets": []any{map[string]any{"entry": "target", "path": ".meta.scope"}}}},
		{ID: wapp.NewID("acme.worker", "target"), Kind: regapi.EntryKind, Meta: map[string]any{}},
	})
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{
		getManifest: func(_ context.Context, org, name, version string) (*ModuleManifest, error) {
			return &ModuleManifest{Org: org, Name: name, Version: version}, nil
		},
	}, Logger: zap.NewNop(), VendorDir: vendor})
	require.NoError(t, err)
	t.Cleanup(handler.manifestCache.Close)
	root := regapi.Entry{ID: regapi.NewID("host.dependencies", "worker"), Kind: regapi.NamespaceDependency,
		Data: payload.New(DependencyDefinition{Component: "acme/worker", Version: "2.0.0",
			Parameters: []Parameter{{Name: "scope", Value: "host:old"}}})}
	cleared := root
	cleared.Data = payload.New(DependencyDefinition{Component: "acme/worker", Version: "2.0.0"})
	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryUpdate, Entry: cleared}, regapi.State{root})
	require.NoError(t, err)
	found := false
	for _, scoped := range result.Additional {
		if scoped.Operation.Entry.ID == regapi.NewID("acme.worker", "target") {
			found = true
			require.Equal(t, "module:default", scoped.Operation.Entry.Meta["scope"])
		}
	}
	require.True(t, found)
}
